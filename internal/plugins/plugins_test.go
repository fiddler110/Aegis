package plugins

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func TestProcessToolExecute(t *testing.T) {
	var command string
	var args []string
	if runtime.GOOS == "windows" {
		command = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command", "Write-Output 'hello from plugin'"}
	} else {
		command = "/bin/sh"
		args = []string{"-c", "echo hello from plugin"}
	}

	pt := &processTool{cfg: ProcessToolConfig{
		Name:       "test-plugin",
		Command:    command,
		Args:       args,
		TimeoutSec: 10,
	}}

	res, err := pt.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello from plugin") {
		t.Errorf("expected output, got %q", res.Content)
	}
}

func TestProcessToolTimeout(t *testing.T) {
	var command string
	var args []string
	if runtime.GOOS == "windows" {
		command = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30"}
	} else {
		command = "/bin/sh"
		args = []string{"-c", "sleep 30"}
	}

	pt := &processTool{cfg: ProcessToolConfig{
		Name:       "slow-plugin",
		Command:    command,
		Args:       args,
		TimeoutSec: 1,
	}}

	res, err := pt.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !res.IsError {
		t.Error("expected timeout error")
	}
	if !strings.Contains(res.Content, "timed out") {
		t.Errorf("expected timeout message, got %q", res.Content)
	}
}

func TestProcessToolFailure(t *testing.T) {
	var command string
	var args []string
	if runtime.GOOS == "windows" {
		command = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command", "exit 1"}
	} else {
		command = "/bin/sh"
		args = []string{"-c", "exit 1"}
	}

	pt := &processTool{cfg: ProcessToolConfig{
		Name:       "fail-plugin",
		Command:    command,
		Args:       args,
		TimeoutSec: 10,
	}}

	res, _ := pt.Execute(context.Background(), nil)
	if !res.IsError {
		t.Error("expected error for failing command")
	}
}

// TestProcessToolCapability verifies Capability() ignores the configured
// value entirely (P42.2): cfg.Capability is untrusted config data (it may
// come from an unreviewed project's .aegis/config.yaml), and Execute always
// runs an arbitrary host command regardless of what it claims — so the
// permission gate must always see CapExecute, never a declared "read"/
// "write"/"network" that would dodge plan-mode/build-mode friction.
func TestProcessToolCapability(t *testing.T) {
	for _, cap := range []string{"read", "write", "network", "execute", ""} {
		pt := &processTool{cfg: ProcessToolConfig{Capability: cap}}
		if got := pt.Capability(); got != tool.CapExecute {
			t.Errorf("cap %q: got %q, want %q (Capability() must ignore config)", cap, got, tool.CapExecute)
		}
	}
}

func TestRegisterProcessTools(t *testing.T) {
	reg := tool.NewRegistry()
	configs := []ProcessToolConfig{
		{Name: "tool-a", Command: "echo", Description: "test tool A"},
		{Name: "", Command: "echo"},   // skipped: no name
		{Name: "tool-b", Command: ""}, // skipped: no command
		{Name: "tool-c", Command: "echo", Description: "test tool C"},
	}
	RegisterProcessTools(reg, configs, nil)

	schemas := reg.Schemas()
	names := make(map[string]bool)
	for _, s := range schemas {
		names[s.Name] = true
	}
	if !names["tool-a"] || !names["tool-c"] {
		t.Errorf("expected tool-a and tool-c registered, got %v", names)
	}
	if names["tool-b"] {
		t.Error("tool-b should not be registered (no command)")
	}
}

func TestProcessToolInputSchema(t *testing.T) {
	pt := &processTool{cfg: ProcessToolConfig{
		InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
	}}
	schema := pt.InputSchema()
	if !strings.Contains(string(schema), `"x"`) {
		t.Errorf("expected custom schema, got %s", schema)
	}

	pt2 := &processTool{cfg: ProcessToolConfig{}}
	schema2 := pt2.InputSchema()
	if string(schema2) != `{"type":"object"}` {
		t.Errorf("expected default schema, got %s", schema2)
	}
}
