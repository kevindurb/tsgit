package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
)

type recordedRequest struct {
	method, url string
	header      http.Header
	body        []byte
}

type fakeDoer struct {
	mu    sync.Mutex
	calls []recordedRequest
	// block, when non-nil, makes Do wait until the channel is closed or the
	// request context is canceled (simulating a hung receiver).
	block chan struct{}
	fn    func(call int, req *http.Request) (int, error)
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	f.mu.Lock()
	f.calls = append(f.calls, recordedRequest{req.Method, req.URL.String(), req.Header.Clone(), body})
	call := len(f.calls) - 1
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	status, err := f.fn(call, req)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}

func (f *fakeDoer) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.calls))
	copy(out, f.calls)
	return out
}

func testDispatcher(t *testing.T, hooks []Hook, doer Doer, env map[string]string) (*Dispatcher, *bytes.Buffer) {
	t.Helper()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d := NewDispatcher(Config{
		Load:    func(context.Context) ([]Hook, error) { return hooks, nil },
		Client:  doer,
		BaseURL: "https://tsgit.example.test",
		Version: "test",
		Log:     logger,
		LookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
	})
	d.backoff = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	d.timeout = 50 * time.Millisecond
	return d, &logBuf
}

func testEvent(changes ...repos.RefChange) transport.PushEvent {
	return transport.PushEvent{
		Repo:    "acme/widget",
		Path:    "/tmp/acme/widget.git",
		Changes: changes,
		Identity: authz.Identity{
			Node:  "node-1",
			Login: "ada@example.com",
		},
	}
}

func updatedChange() repos.RefChange {
	return repos.RefChange{
		Ref:  "refs/heads/main",
		Kind: repos.RefUpdated,
		Old:  strings.Repeat("a", 40),
		New:  strings.Repeat("b", 40),
	}
}

func TestDeliverySendsGitHubPushPayload(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local:8080/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()

	calls := doer.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.method != http.MethodPost {
		t.Errorf("method = %q", call.method)
	}
	for k, want := range map[string]string{
		"Content-Type":   "application/json",
		"X-GitHub-Event": "push",
	} {
		if got := call.header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := call.header.Get("X-GitHub-Delivery"); len(got) != 36 {
		t.Errorf("X-GitHub-Delivery = %q, want a UUID", got)
	}
	if ua := call.header.Get("User-Agent"); !strings.Contains(ua, "tsgit/") {
		t.Errorf("User-Agent = %q", ua)
	}
	if sig := call.header.Get("X-Hub-Signature-256"); sig != "" {
		t.Errorf("unsigned hook got signature header %q", sig)
	}
	var body map[string]any
	if err := json.Unmarshal(call.body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := map[string]any{
		"ref":     "refs/heads/main",
		"before":  strings.Repeat("a", 40),
		"after":   strings.Repeat("b", 40),
		"created": false,
		"deleted": false,
		"forced":  false,
	}
	for k, w := range want {
		if body[k] != w {
			t.Errorf("body[%q] = %v, want %v", k, body[k], w)
		}
	}
	repo, ok := body["repository"].(map[string]any)
	if !ok {
		t.Fatalf("repository missing from body: %v", body)
	}
	if repo["name"] != "widget" || repo["full_name"] != "acme/widget" || repo["clone_url"] != "https://tsgit.example.test/acme/widget.git" {
		t.Errorf("repository = %v", repo)
	}
	pusher, ok := body["pusher"].(map[string]any)
	if !ok || pusher["name"] != "ada@example.com" {
		t.Errorf("pusher = %v", body["pusher"])
	}
}

func TestDeliveryDeletedRefUsesZeroOID(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(repos.RefChange{
		Ref: "refs/heads/old", Kind: repos.RefDeleted,
		Old: strings.Repeat("a", 40), New: strings.Repeat("0", 40),
	}))
	d.Wait()
	body := doer.recorded()[0].body
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["after"] != strings.Repeat("0", 40) {
		t.Errorf("after = %v", parsed["after"])
	}
	if parsed["deleted"] != true {
		t.Errorf("deleted = %v", parsed["deleted"])
	}
}

func TestSigningMatchesReferenceHMAC(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	hook := Hook{Repos: []string{"*/*"}, URL: "http://ci.local/hook", SecretEnv: "TSC_TEST_SECRET"}
	d, _ := testDispatcher(t, []Hook{hook}, doer, map[string]string{"TSC_TEST_SECRET": "s3cret"})
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()

	call := doer.recorded()[0]
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(call.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := call.header.Get("X-Hub-Signature-256"); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
}

func TestSecretEnvMissingFailsClosed(t *testing.T) {
	for _, name := range []string{"missing", "empty"} {
		env := map[string]string{}
		if name == "empty" {
			env["TSC_TEST_SECRET"] = ""
		}
		doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
		hook := Hook{Repos: []string{"*/*"}, URL: "http://ci.local/hook", SecretEnv: "TSC_TEST_SECRET"}
		d, logs := testDispatcher(t, []Hook{hook}, doer, env)
		d.Dispatch(testEvent(updatedChange()))
		d.Wait()
		if got := len(doer.recorded()); got != 0 {
			t.Fatalf("%s: %d deliveries, want 0 (fail closed)", name, got)
		}
		if !strings.Contains(logs.String(), "TSC_TEST_SECRET") {
			t.Errorf("%s: log does not name the variable: %s", name, logs.String())
		}
		if strings.Contains(logs.String(), "s3cret") {
			t.Errorf("%s: log leaks the secret value: %s", name, logs.String())
		}
	}
}

func TestRetriesUntilSuccess(t *testing.T) {
	n := 0
	doer := &fakeDoer{fn: func(call int, _ *http.Request) (int, error) {
		n = call + 1
		if call < 2 {
			return http.StatusInternalServerError, nil
		}
		return http.StatusNoContent, nil
	}}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
}

func TestGivesUpAfterThreeAttempts(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusBadGateway, nil }}
	d, logs := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if got := len(doer.recorded()); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if !strings.Contains(logs.String(), "giving up") {
		t.Errorf("expected final failure to be logged: %s", logs.String())
	}
}

func TestRetryDeliveriesShareDeliveryID(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusInternalServerError, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	ids := map[string]bool{}
	for _, call := range doer.recorded() {
		ids[call.header.Get("X-GitHub-Delivery")] = true
	}
	if len(ids) != 1 {
		t.Fatalf("delivery ids across retries = %v, want 1", ids)
	}
}

func TestTimeoutCountsAsFailedAttempt(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	doer := &fakeDoer{block: block, fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if got := len(doer.recorded()); got != 3 {
		t.Fatalf("attempts = %d, want 3 (timeouts retry)", got)
	}
}

func TestRedirectIsNotFollowedAndCountsAsFailure(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusFound, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if got := len(doer.recorded()); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestConcurrencyLimitDropsOverflow(t *testing.T) {
	block := make(chan struct{})
	doer := &fakeDoer{block: block, fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	hooks := []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}
	d, _ := testDispatcher(t, hooks, doer, nil)
	changes := make([]repos.RefChange, 0, 12)
	for i := 0; i < 12; i++ {
		changes = append(changes, repos.RefChange{
			Ref: fmt.Sprintf("refs/heads/b%02d", i), Kind: repos.RefUpdated,
			Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40),
		})
	}
	d.Dispatch(testEvent(changes...))
	// Wait for all goroutines that got a slot; the semaphore caps in-flight at 8.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(doer.recorded()) < 8 {
		time.Sleep(time.Millisecond)
	}
	if got := len(doer.recorded()); got != 8 {
		t.Fatalf("in-flight = %d, want 8", got)
	}
	if got := d.Dropped(); got != 4 {
		t.Fatalf("dropped = %d, want 4", got)
	}
	close(block)
	d.Wait()
}

func TestOneDeliveryPerChangedRef(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	hooks := []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}
	d, _ := testDispatcher(t, hooks, doer, nil)
	changes := []repos.RefChange{
		{Ref: "refs/heads/a", Kind: repos.RefUpdated, Old: strings.Repeat("1", 40), New: strings.Repeat("2", 40)},
		{Ref: "refs/heads/b", Kind: repos.RefUpdated, Old: strings.Repeat("3", 40), New: strings.Repeat("4", 40), Forced: true},
		{Ref: "refs/tags/v1", Kind: repos.RefCreated, Old: strings.Repeat("0", 40), New: strings.Repeat("5", 40)},
	}
	d.Dispatch(testEvent(changes...))
	d.Wait()
	calls := doer.recorded()
	if len(calls) != 3 {
		t.Fatalf("deliveries = %d, want 3", len(calls))
	}
	refs := map[string]bool{}
	forced := map[string]bool{}
	for _, c := range calls {
		var body map[string]any
		_ = json.Unmarshal(c.body, &body)
		refs[body["ref"].(string)] = true
		forced[body["ref"].(string)] = body["forced"].(bool)
	}
	if !refs["refs/heads/a"] || !refs["refs/heads/b"] || !refs["refs/tags/v1"] {
		t.Errorf("refs delivered = %v", refs)
	}
	if !forced["refs/heads/b"] || forced["refs/heads/a"] {
		t.Errorf("forced flags = %v", forced)
	}
}

func TestNoHooksNoDeliveries(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, nil, doer, nil)
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if got := len(doer.recorded()); got != 0 {
		t.Fatalf("deliveries = %d, want 0", got)
	}
}

func TestLoadFailureDeliversNothing(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	var logBuf bytes.Buffer
	d := NewDispatcher(Config{
		Load:    func(context.Context) ([]Hook, error) { return nil, fmt.Errorf("policy unreachable") },
		Client:  doer,
		BaseURL: "https://tsgit.example.test",
		Version: "test",
		Log:     slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	d.Dispatch(testEvent(updatedChange()))
	d.Wait()
	if got := len(doer.recorded()); got != 0 {
		t.Fatalf("deliveries = %d, want 0 when policy load fails", got)
	}
	if !strings.Contains(logBuf.String(), "policy") {
		t.Errorf("log does not mention policy failure: %s", logBuf.String())
	}
}

func TestPusherFallsBackToNodeName(t *testing.T) {
	doer := &fakeDoer{fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	ev := testEvent(updatedChange())
	ev.Identity = authz.Identity{Node: "tagged-ci-node"}
	d.Dispatch(ev)
	d.Wait()
	var body map[string]any
	_ = json.Unmarshal(doer.recorded()[0].body, &body)
	if got := body["pusher"].(map[string]any)["name"]; got != "tagged-ci-node" {
		t.Errorf("pusher.name = %v, want tagged-ci-node", got)
	}
}

func TestOnPushDoesNotBlockThePushPath(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	doer := &fakeDoer{block: block, fn: func(int, *http.Request) (int, error) { return http.StatusOK, nil }}
	d, _ := testDispatcher(t, []Hook{{Repos: []string{"*/*"}, URL: "http://ci.local/hook"}}, doer, nil)
	start := time.Now()
	d.OnPush(context.Background(), testEvent(updatedChange()))
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("OnPush blocked for %s with a hanging receiver", elapsed)
	}
	d.Wait() // let the blocked goroutine observe the listener timeout path; close(block) releases it
}
