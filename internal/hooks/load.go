package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"tailscale.com/ipn/ipnstate"
)

// StatusReader is the narrow slice of the LocalAPI the loader needs: the
// node's own status, including its capability map.
type StatusReader interface {
	StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error)
}

// LoadFunc reads the current hook set from the policy. It matches the Loader
// signature expected by the dispatcher and is re-evaluated on every push, so
// a policy edit takes effect on the next push without a restart.
type LoadFunc func(ctx context.Context) ([]Hook, error)

// StatusLoader returns a LoadFunc backed by the LocalAPI of the embedded
// node. A status failure is an error (the push itself is never affected; the
// caller logs and sends nothing). A missing cap/hooks attribute yields no
// hooks and no error.
func StatusLoader(status StatusReader, log *slog.Logger) LoadFunc {
	return func(ctx context.Context) ([]Hook, error) {
		st, err := status.StatusWithoutPeers(ctx)
		if err != nil {
			return nil, fmt.Errorf("read node status: %w", err)
		}
		if st == nil || st.Self == nil {
			return nil, errors.New("node status has no self node")
		}
		return ParseCap(st.Self.CapMap, log), nil
	}
}
