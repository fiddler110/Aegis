package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
)

// daemonHealthTimeout bounds the "is one already running?" probe, and
// daemonReadyTimeout bounds the wait for one this process just started. Both
// were spelled inline and identically at every bootstrap site.
const (
	daemonHealthTimeout = 2 * time.Second
	daemonReadyTimeout  = 10 * time.Second
)

// connectOrStartDaemon returns a client for the configured daemon, starting an
// embedded one in this process if none is reachable.
//
// embedded reports which of those happened, for callers whose output or
// lifetime differs between "attached to yours" and "running one for you"
// (`aegis ui` says "Ctrl+C to stop the daemon" only in the second case).
//
// cleanup is never nil and must always be deferred. It scrubs the client's
// bearer token (FIND-33/P24.21) and then stops the embedded daemon if one was
// started, which is the order the four hand-written copies of this achieved
// through defer's LIFO ordering. Client.Zero is idempotent, so a caller that
// hands the client to something which scrubs it too — tui.Run does — can defer
// this as well without harm.
//
// CLN-4: this was ~28 verbatim lines in cli/acp.go, cli/mcpserve.go, cli/ui.go
// and cli/root.go. All four remembered the token scrub; the concern is the
// fifth command, where "all four remembered" stops being evidence of anything.
// Folding the health probe, the embedded start, the re-dial and the scrub into
// one place makes the scrub structural instead of remembered.
func connectOrStartDaemon(ctx context.Context, cfg *config.Config) (cl *client.Client, embedded bool, cleanup func(), err error) {
	// A missing TLS cert here just means no daemon has ever started at this
	// data dir yet (server.New generates it) — treat it the same as "no daemon
	// reachable", not as fatal.
	cl, clErr := client.NewFromConfig(cfg)

	reachable := false
	if clErr == nil {
		healthCtx, healthCancel := context.WithTimeout(ctx, daemonHealthTimeout)
		reachable = cl.Health(healthCtx) == nil
		healthCancel()
	}
	if reachable {
		return cl, false, func() { cl.Zero() }, nil
	}

	stopDaemon, startErr := startEmbeddedDaemon(cfg)
	if startErr != nil {
		return nil, false, func() {}, fmt.Errorf("start daemon: %w", startErr)
	}

	// Rebuild the client before polling, not after: startEmbeddedDaemon has now
	// written daemon.token and (with server.tls.enabled) generated daemon.crt,
	// neither of which existed when cl was built above on a first run. From
	// here on a load failure is a real bug — the daemon we just started
	// guarantees both files exist — so it is fatal rather than folded into
	// "keep waiting".
	cl, clErr = client.NewFromConfig(cfg)
	if clErr != nil {
		stopDaemon()
		return nil, false, func() {}, fmt.Errorf("daemon started but client could not be configured: %w", clErr)
	}
	cleanup = func() {
		cl.Zero()
		stopDaemon()
	}
	if !waitForDaemon(cl, daemonReadyTimeout) {
		cleanup()
		return nil, false, func() {}, fmt.Errorf("daemon at %s did not become ready within %s", cfg.Server.Addr, daemonReadyTimeout)
	}
	return cl, true, cleanup, nil
}
