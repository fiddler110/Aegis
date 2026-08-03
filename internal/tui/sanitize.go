package tui

import "github.com/fiddler110/aegis/internal/termsafe"

// The sanitizers themselves live in internal/termsafe: the plain CLI renderer
// (internal/cli/chat_render.go) writes the same two classes of text — model
// prose and raw tool output — to the same kind of terminal, and a second copy
// of this logic is the shape of bug this codebase has already paid for once
// (the duplicated OCI hardening-flag list, P55.7). These thin aliases keep the
// TUI's call sites unchanged; the behavior tests moved with the code, to
// internal/termsafe/termsafe_test.go.

func stripControlSeqs(s string) string { return termsafe.StripControlSeqs(s) }

func stripDangerousSeqs(s string) string { return termsafe.StripDangerousSeqs(s) }
