package lsp

import (
	"fmt"
	"strings"
)

// trustedCommands is a small allowlist of common LSP server binary basenames.
// LSP server configs can come from project-level .aegis/config.yaml, which a
// malicious repo could commit; since all configured LSP servers are started
// eagerly at daemon boot (before any interactive approver exists), there is
// no live human to prompt for a TOFU-style confirmation. Instead, a command
// whose basename matches this list is allowed to start automatically; any
// other command requires the operator to explicitly opt in via the server's
// `trust: true` config field.
//
// Matching is against the basename only (directory components and, on
// Windows, a .exe/.bat/.cmd extension stripped) — the point is recognizing
// that the configured value names a well-known LSP tool, not vouching for
// which binary actually answers to that name on the resolved PATH.
var trustedCommands = map[string]bool{
	"gopls":                      true,
	"typescript-language-server": true,
	"pyright-langserver":         true,
	"pyright":                    true,
	"pylsp":                      true,
	"rust-analyzer":              true,
	"clangd":                     true,
	"jdtls":                      true,
	"solargraph":                 true,
	"ruby-lsp":                   true,
	"sourcekit-lsp":              true,
	"lua-language-server":        true,
	"omnisharp":                  true,
	"elixir-ls":                  true,
	"zls":                        true,
	"terraform-ls":               true,
	"yaml-language-server":       true,
	"bash-language-server":       true,
	"vscode-json-languageserver": true,
	"intelephense":               true,
}

// commandBasename normalizes a configured LSP command for allowlist
// comparison: strips any directory path, lowercases, and strips a Windows
// executable extension. A config's command can be a Windows-style path
// (`C:\tools\gopls.exe`) regardless of the OS aegis itself runs on, so this
// splits on both `/` and `\` explicitly rather than using filepath.Base —
// filepath's separator handling follows the build's GOOS, not the string's.
func commandBasename(command string) string {
	base := command
	if idx := strings.LastIndexAny(base, `/\`); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.ToLower(base)
	switch {
	case strings.HasSuffix(base, ".exe"), strings.HasSuffix(base, ".bat"), strings.HasSuffix(base, ".cmd"):
		base = base[:len(base)-4]
	}
	return base
}

// isTrustedCommand reports whether command's basename is in the built-in
// LSP server allowlist.
func isTrustedCommand(command string) bool {
	return trustedCommands[commandBasename(command)]
}

// checkTrusted decides whether an LSP server config is allowed to start: its
// command basename must be in the built-in allowlist, or the config must
// carry an explicit Trust opt-in. It is pure (does not spawn a process) so
// it can be unit tested without a real binary present.
func checkTrusted(cfg ServerConfig) error {
	if cfg.Trust {
		return nil
	}
	if isTrustedCommand(cfg.Command) {
		return nil
	}
	return fmt.Errorf(
		"lsp: command %q (server %q) is not in the built-in trusted allowlist and no explicit opt-in was given; "+
			"a project or user config can point lsp[].command at an arbitrary binary, so unrecognized commands are "+
			"refused by default — if this is a legitimate LSP server, add `trust: true` to this server's entry in "+
			"the `lsp:` config list to opt in explicitly",
		cfg.Command, cfg.Name,
	)
}
