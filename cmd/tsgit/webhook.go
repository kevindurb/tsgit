package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/hooks"
)

// devWebhooks lets a test inject a dispatcher into the development server.
// Nil means the default wiring: a dispatcher with no policy source, so pushes
// behave exactly as before.
var devWebhooks *hooks.Dispatcher

// newDevWebhooks builds the dispatcher for development mode. There is no
// tailnet in dev mode, hence no policy source; deliveries never fire unless a
// test injects devWebhooks.
func newDevWebhooks(cfg *config.Config, logger *slog.Logger) *hooks.Dispatcher {
	if devWebhooks != nil {
		return devWebhooks
	}
	logger.Debug("dev mode: webhook delivery disabled (no tailnet policy source)")
	return hooks.NewDispatcher(hooks.Config{
		Client:  http.DefaultClient,
		BaseURL: "http://" + cfg.DevListen,
		Version: version,
		Log:     logger,
	})
}

// newTailnetWebhooks builds the production dispatcher. Delivery goes through
// the node's own HTTP client, so it never touches the host network, and the
// hook set is re-read from the node's policy on every push.
func newTailnetWebhooks(node *tailnet, baseURL string, logger *slog.Logger) *hooks.Dispatcher {
	return hooks.NewDispatcher(hooks.Config{
		Load:    hooks.StatusLoader(node, logger),
		Client:  node.HTTPClient(),
		BaseURL: baseURL,
		Version: version,
		Log:     logger,
	})
}

// cloneBaseURL renders the base URL a pusher can clone from. Port is omitted
// when it matches the scheme default.
func cloneBaseURL(scheme, host string, port int) string {
	switch {
	case port == 0 || (scheme == "https" && port == 443) || (scheme == "http" && port == 80):
		return fmt.Sprintf("%s://%s", scheme, host)
	default:
		return fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}
}
