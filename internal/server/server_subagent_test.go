package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// webFetchProbeAdapter calls web_fetch on its first turn, then reports back
// whether that call was denied (its ToolResultBlock came back IsError) so the
// test can assert on the final text without inspecting engine internals.
type webFetchProbeAdapter struct {
	mu    sync.Mutex
	calls int
}

func (a *webFetchProbeAdapter) Name() string { return "webfetch-probe" }

func (a *webFetchProbeAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	ch := make(chan provider.Event, 4)
	if n == 0 {
		input := json.RawMessage(`{"url":"https://example.com"}`)
		ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu1", Name: "web_fetch", Input: input}}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{}}
		close(ch)
		return ch, nil
	}

	denied := false
	if last := req.Messages[len(req.Messages)-1]; last.Role == provider.RoleUser {
		for _, b := range last.Content {
			if tr, ok := b.(provider.ToolResultBlock); ok && tr.ToolUseID == "tu1" && tr.IsError {
				denied = true
			}
		}
	}
	if denied {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "BLOCKED"}
	} else {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: "LEAKED"}
	}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

// TestSubAgentRunnerAppliesOperatorDenyRule is the regression for P10.1: a
// sub-agent spawned via subAgentRunner must be bound by the same text
// allow/deny rules and contextual policy a top-level run gets from newEngine,
// not just the bare mode gate. Before the fix, subAgentRunner built
// permission.New(mode, approver) directly, so an operator's `deny
// web_fetch(*)` rule (or an egress-then-write / network-allowlist policy)
// never reached a spawned teammate's tool calls — a data-exfil path straight
// through the P7.2/P7.3 hardening. In build mode the bare mode gate allows
// network capability unconditionally, so this test fails against the old
// code (the probe reports "LEAKED") and passes once subAgentRunner shares
// newEngine's gate stack via buildGate.
func TestSubAgentRunnerAppliesOperatorDenyRule(t *testing.T) {
	root := t.TempDir()
	store, err := session.Open(filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: root}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	adapter := &webFetchProbeAdapter{}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, reg)

	rules, err := permission.ParseRules([]string{"deny web_fetch(*)"})
	if err != nil {
		t.Fatal(err)
	}
	srv.permRules = rules

	out, err := srv.subAgentRunner()(context.Background(), swarm.SpawnConfig{
		Name:   "teammate",
		Prompt: "fetch a url",
		Mode:   "build",
	})
	if err != nil {
		t.Fatalf("subAgentRunner returned error: %v", err)
	}
	if !strings.Contains(out, "BLOCKED") {
		t.Fatalf("expected sub-agent's web_fetch call to be blocked by the operator's deny rule, got output: %q", out)
	}
}

// TestSubAgentRunnerUsesSpawnConfigWorkdir is the P25.8 regression for gap
// (a)'s foreground case: subAgentRunner must set engine.Options.Workdir from
// cfg.Workdir explicitly, not rely on the parent session's tool.WithWorkdir
// ctx value leaking through the spawn's context chain — a background/
// detached spawn's job runs under a context derived from
// context.Background() (task.Manager.Start) and would otherwise silently
// fall back to the daemon's default workspace regardless of cfg.Workdir.
// This test drives subAgentRunner directly with a bare context (no
// tool.WithWorkdir set), so it only passes if cfg.Workdir itself reaches the
// spawned engine.
func TestSubAgentRunnerUsesSpawnConfigWorkdir(t *testing.T) {
	daemonRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(daemonRoot, "target.txt"), []byte("daemon-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherDir, "target.txt"), []byte("other-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: daemonRoot}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store,
		&toolResultEchoAdapter{path: "target.txt"}, reg)

	// Deliberately a bare context — no tool.WithWorkdir set — so the only way
	// the spawned engine reads otherDir is via cfg.Workdir itself.
	out, err := srv.subAgentRunner()(context.Background(), swarm.SpawnConfig{
		Name:    "teammate",
		Prompt:  "read target.txt",
		Mode:    "build",
		Workdir: otherDir,
	})
	if err != nil {
		t.Fatalf("subAgentRunner returned error: %v", err)
	}
	if !strings.Contains(out, "other-content") {
		t.Errorf("sub-agent output = %q, want the fixture content from cfg.Workdir (%q)", out, otherDir)
	}
	if strings.Contains(out, "daemon-content") {
		t.Errorf("sub-agent output = %q, must not read the daemon's default workspace", out)
	}
}
