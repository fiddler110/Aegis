package lsp

import (
	"strings"
	"testing"
)

func TestCheckTrusted_AllowlistedBasename(t *testing.T) {
	cfg := ServerConfig{Name: "gopls", Command: "gopls", Trust: false}
	if err := checkTrusted(cfg); err != nil {
		t.Fatalf("expected allowlisted command to be trusted, got error: %v", err)
	}
}

func TestCheckTrusted_AllowlistedBasenameViaFullPath(t *testing.T) {
	cases := []string{
		"/usr/local/bin/gopls",
		`C:\tools\gopls.exe`,
		`C:\tools\GOPLS.EXE`,
		"/usr/bin/pyright-langserver",
	}
	for _, command := range cases {
		cfg := ServerConfig{Name: "test", Command: command, Trust: false}
		if err := checkTrusted(cfg); err != nil {
			t.Errorf("command %q: expected trusted via basename match, got error: %v", command, err)
		}
	}
}

func TestCheckTrusted_UnknownCommandRefused(t *testing.T) {
	cfg := ServerConfig{Name: "evil", Command: "/tmp/definitely-not-an-lsp-server", Trust: false}
	err := checkTrusted(cfg)
	if err == nil {
		t.Fatal("expected non-allowlisted command with Trust=false to be refused")
	}
	if !strings.Contains(err.Error(), "trust") || !strings.Contains(err.Error(), "lsp[]") {
		t.Errorf("expected error to mention opt-in guidance, got: %v", err)
	}
}

func TestCheckTrusted_UnknownCommandWithExplicitTrust(t *testing.T) {
	cfg := ServerConfig{Name: "custom", Command: "/opt/my-custom-lsp", Trust: true}
	if err := checkTrusted(cfg); err != nil {
		t.Fatalf("expected explicit Trust=true to allow unknown command, got error: %v", err)
	}
}

func TestIsTrustedCommand_CaseInsensitive(t *testing.T) {
	if !isTrustedCommand("GoPls") {
		t.Error("expected case-insensitive match for GoPls")
	}
	if !isTrustedCommand("RUST-ANALYZER") {
		t.Error("expected case-insensitive match for RUST-ANALYZER")
	}
}

func TestIsTrustedCommand_DoesNotMatchFullPathLiterally(t *testing.T) {
	// Sanity check: matching happens on basename, not by accident matching
	// substrings of an untrusted full path that merely contains a trusted name.
	if isTrustedCommand("/tmp/not-gopls-actually-evil/gopls-wrapper") {
		t.Error("expected basename-only match, not a substring match")
	}
}
