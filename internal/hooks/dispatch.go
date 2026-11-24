package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
)

// Doer performs an HTTP request. It is satisfied by *http.Client; tests
// substitute a fake. The production client is built on the embedded tsnet
// node so requests never touch the host network.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config wires a Dispatcher.
type Config struct {
	// Load resolves the current hook set. Called once per push, so policy
	// edits take effect without a restart. Required.
	Load func(ctx context.Context) ([]Hook, error)
	// Client delivers requests. Required; production uses the tsnet client.
	Client Doer
	// BaseURL is the node's clone base, used to build repository.clone_url.
	BaseURL string
	// Version is reported in the User-Agent header.
	Version string
	// Log defaults to slog.Default.
	Log *slog.Logger
	// LookupEnv resolves hook secret variables; defaults to os.LookupEnv.
	LookupEnv func(string) (string, bool)
}

// Dispatcher delivers push events to matching hooks, best effort. All state is
// in memory; deliveries are never persisted and pending retries are lost on
// shutdown.
type Dispatcher struct {
	cfg       Config
	log       *slog.Logger
	lookupEnv func(string) (string, bool)

	sem     chan struct{}
	wg      sync.WaitGroup
	dropped atomic.Uint64
	backoff []time.Duration
	timeout time.Duration
}

// deliveryConcurrency bounds how many deliveries (or retries) are in flight.
const deliveryConcurrency = 8

// NewDispatcher builds a Dispatcher with delivery defaults: three attempts in
// total (one initial, two retries after 1s and 5s), 5s per attempt.
func NewDispatcher(cfg Config) *Dispatcher {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	lookup := cfg.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return &Dispatcher{
		cfg:       cfg,
		log:       log,
		lookupEnv: lookup,
		sem:       make(chan struct{}, deliveryConcurrency),
		backoff:   []time.Duration{time.Second, 5 * time.Second},
		timeout:   5 * time.Second,
	}
}

// Dropped reports how many deliveries were dropped because the concurrency
// budget was exhausted. Exposed for tests and diagnostics.
func (d *Dispatcher) Dropped() uint64 { return d.dropped.Load() }

// Wait blocks until every delivery spawned so far has finished. Tests use it;
// production code never waits on it.
func (d *Dispatcher) Wait() { d.wg.Wait() }

// OnPush implements transport.PushObserver. It returns immediately; matching,
// payload construction, signing, and delivery all happen in the background.
func (d *Dispatcher) OnPush(_ context.Context, ev transport.PushEvent) {
	go d.Dispatch(ev)
}

// Dispatch loads the current hook set and fans deliveries out. It returns
// once every delivery goroutine has been scheduled; call Wait to block until
// they finish. Tests call Dispatch directly to avoid racing the scheduler.
func (d *Dispatcher) Dispatch(ev transport.PushEvent) {
	if d.cfg.Load == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hooks, err := d.cfg.Load(ctx)
	if err != nil {
		d.log.Warn("webhook: could not load hooks from policy; push not notified", "repo", ev.Repo, "error", err)
		return
	}
	for _, h := range hooks {
		if !h.Matches(ev.Repo) {
			continue
		}
		for _, change := range ev.Changes {
			select {
			case d.sem <- struct{}{}:
			default:
				d.dropped.Add(1)
				d.log.Warn("webhook delivery dropped: concurrency limit reached",
					"repo", ev.Repo, "ref", change.Ref, "url", safeURL(h.URL))
				continue
			}
			d.wg.Add(1)
			go func(h Hook, change repos.RefChange) {
				defer d.wg.Done()
				defer func() { <-d.sem }()
				d.deliver(h, ev, change)
			}(h, change)
		}
	}
}

func (d *Dispatcher) deliver(h Hook, ev transport.PushEvent, change repos.RefChange) {
	pusher := ev.Identity.Login
	if pusher == "" {
		pusher = ev.Identity.Node
	}
	body, err := buildPushPayload(d.cfg.BaseURL, ev.Repo, change, pusher)
	if err != nil {
		d.log.Error("webhook: build payload", "repo", ev.Repo, "error", err)
		return
	}
	signature := ""
	if h.SecretEnv != "" {
		secret, ok := d.lookupEnv(h.SecretEnv)
		if !ok || secret == "" {
			// Fail closed: never send unsigned when the operator asked for signing.
			d.log.Error("webhook delivery not sent: secretEnv variable missing or empty",
				"secretEnv", h.SecretEnv, "repo", ev.Repo, "ref", change.Ref, "url", safeURL(h.URL))
			return
		}
		signature = signBody([]byte(secret), body)
	}
	deliveryID, err := newDeliveryID()
	if err != nil {
		d.log.Error("webhook: generate delivery id", "error", err)
		return
	}

	attempts := 1 + len(d.backoff)
	for attempt := 1; ; attempt++ {
		if attempt > 1 {
			timer := time.NewTimer(d.backoff[attempt-2])
			<-timer.C
		}
		status, err := d.attempt(h, body, signature, deliveryID)
		if err == nil && status >= 200 && status < 300 {
			d.log.Debug("webhook delivered",
				"repo", ev.Repo, "ref", change.Ref, "url", safeURL(h.URL), "status", status, "attempt", attempt)
			return
		}
		if attempt == attempts {
			d.log.Error("webhook delivery failed; giving up",
				"repo", ev.Repo, "ref", change.Ref, "url", safeURL(h.URL), "attempts", attempt, "error", errString(err, status))
			return
		}
		d.log.Warn("webhook delivery attempt failed; will retry",
			"repo", ev.Repo, "ref", change.Ref, "url", safeURL(h.URL), "attempt", attempt, "backoff", d.backoff[attempt-1], "error", errString(err, status))
	}
}

func (d *Dispatcher) attempt(h Hook, body []byte, signature, deliveryID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tsgit/"+d.cfg.Version)
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	resp, err := d.cfg.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

func errString(err error, status int) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("unexpected status %d", status)
}

// safeURL strips userinfo, query, and fragment so credentials embedded in a
// hook URL never reach the logs.
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
