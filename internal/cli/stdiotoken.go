package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/fiddler110/aegis/internal/config"
)

// resolveStdioAuthToken returns the shared-secret token that gates a stdio
// JSON-RPC interface (`aegis mcp-serve` / `aegis acp`) against an
// unauthenticated caller (P27.4/FIND-06).
//
// Before this, AEGIS_MCP_TOKEN/AEGIS_ACP_TOKEN were opt-in: if the launching
// harness's environment didn't set one, the interface ran fully
// unauthenticated — any local process able to write to the subprocess's
// stdin could drive full agent turns. Now a token is always required: if
// envVar is set, that value wins unconditionally (an operator's explicit
// choice always overrides the default, preserving today's behavior for
// existing integrations that already set it). Otherwise a fresh random
// token is generated via config.GenerateAndWriteToken and written to
// tokenPath — owner-only permissions, mirroring the daemon's own
// daemon.token bootstrap (internal/server) — so the launching integration
// can read it from a well-known, deterministic location after spawning the
// subprocess, the same way a TUI client reads daemon.token to authenticate
// to an auto-started daemon.
//
// The returned token is never empty.
func resolveStdioAuthToken(envVar, tokenPath, label string, logger *slog.Logger) (string, error) {
	if v := os.Getenv(envVar); v != "" {
		logger.Info(label+": authentication required", "source", "env:"+envVar)
		return v, nil
	}
	token, err := config.GenerateAndWriteToken(tokenPath)
	if err != nil {
		return "", fmt.Errorf("generate %s auth token: %w", label, err)
	}
	logger.Info(label+": authentication required", "source", "auto-generated", "token_path", tokenPath)
	return token, nil
}
