package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

// TestCommandDefsWellFormed is the P14.10 regression: every entry in the
// single source-of-truth table has a name, a handler, and detailed help, and
// no name is duplicated (which would silently shadow one command's dispatch
// entry with another's in the map built from this table).
func TestCommandDefsWellFormed(t *testing.T) {
	seen := make(map[string]bool)
	for _, c := range commandDefs() {
		if c.name == "" {
			t.Error("commandDef with empty name")
		}
		if c.handler == nil {
			t.Errorf("%q has no handler", c.name)
		}
		if c.shortDesc == "" {
			t.Errorf("%q has no shortDesc", c.name)
		}
		if c.detailedHelp == "" {
			t.Errorf("%q has no detailedHelp", c.name)
		}
		if seen[c.name] {
			t.Errorf("%q is duplicated in commandDefs", c.name)
		}
		seen[c.name] = true
	}
}

// TestBuiltinCommandsCoverDispatchTable guards against the P14.1 drift: a
// command dispatchable via d.builtins (and listed in /help) but absent from
// builtinCommands never appears in the completion popup or command palette,
// so typing "/" + its prefix surfaces nothing — precisely how
// /security-config, /scan, /debate, /rollback, /detach, /archive, and /humor
// went silently unreachable. "quit" is a deliberate exception (bare alias for
// "exit", not listed separately), matching TestSlashCommandsAreListedInHelp.
func TestBuiltinCommandsCoverDispatchTable(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	listed := make(map[string]bool, len(builtinCommands))
	for _, e := range builtinCommands {
		listed[e.name] = true
	}
	for name := range d.builtins {
		if name == "quit" {
			continue
		}
		if !listed[name] {
			t.Errorf("/%s is dispatchable but missing from builtinCommands (completion popup/palette)", name)
		}
	}
}

func names(items []cmdEntry) []string {
	out := make([]string, len(items))
	for i, e := range items {
		out[i] = e.name
	}
	return out
}

func TestComputeCompletion(t *testing.T) {
	all := builtinCommands

	tests := []struct {
		name       string
		value      string
		wantActive bool
		wantFirst  string // first match name when active
	}{
		{"plain text inactive", "hello", false, ""},
		{"empty inactive", "", false, ""},
		{"bare slash shows all", "/", true, "help"},
		{"prefix match", "/mo", true, "mode"},
		{"exact name", "/clear", true, "clear"},
		{"space closes popup", "/mode ", false, ""},
		{"newline closes popup", "/mode\n", false, ""},
		{"no match inactive", "/zzzzz", false, ""},
		{"substring matches description", "/permission", true, "mode"}, // "permission" is in /mode's description
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCompletion(tt.value, all, nil)
			if got.active != tt.wantActive {
				t.Fatalf("active = %v, want %v (items=%v)", got.active, tt.wantActive, names(got.items))
			}
			if tt.wantActive {
				if len(got.items) == 0 {
					t.Fatalf("active but no items")
				}
				if got.items[0].name != tt.wantFirst {
					t.Errorf("first match = %q, want %q (all=%v)", got.items[0].name, tt.wantFirst, names(got.items))
				}
			}
		})
	}
}

func TestComputeCompletionDescSubstring(t *testing.T) {
	// "transcript" appears in the description of /clear but not its name.
	got := computeCompletion("/transcript", builtinCommands, nil)
	if !got.active {
		t.Fatalf("expected active for description substring match")
	}
	if got.items[0].name != "clear" {
		t.Errorf("first match = %q, want clear", got.items[0].name)
	}
}

func TestFileCompletion(t *testing.T) {
	files := []string{"internal/tui/tui.go", "internal/tui/theme.go", "README.md"}

	tests := []struct {
		name       string
		value      string
		wantActive bool
		wantKind   completionKind
		wantFirst  string
		wantStart  int
	}{
		{"bare at lists ref types first", "@", true, compFile, "image:", 0},
		{"ref-type prefix", "@diag", true, compFile, "diagnostics", 0},
		{"base-name prefix", "@theme", true, compFile, "internal/tui/theme.go", 0},
		{"path prefix", "@internal/tui/tu", true, compFile, "internal/tui/tui.go", 0},
		{"mid-sentence mention", "look at @README", true, compFile, "README.md", 8},
		{"at without whitespace before is inactive", "foo@bar", false, compSlash, "", 0},
		{"completed mention closes", "@README.md ", false, compSlash, "", 0},
		{"ref value being typed closes", "@image:src/x", false, compSlash, "", 0},
		{"no file match inactive", "@zzzz", false, compSlash, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCompletion(tt.value, builtinCommands, files)
			if got.active != tt.wantActive {
				t.Fatalf("active = %v, want %v", got.active, tt.wantActive)
			}
			if !tt.wantActive {
				return
			}
			if got.kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", got.kind, tt.wantKind)
			}
			if got.items[0].name != tt.wantFirst {
				t.Errorf("first = %q, want %q (all=%v)", got.items[0].name, tt.wantFirst, names(got.items))
			}
			if got.tokenStart != tt.wantStart {
				t.Errorf("tokenStart = %d, want %d", got.tokenStart, tt.wantStart)
			}
		})
	}
}

func TestCompletionMoveWraps(t *testing.T) {
	c := computeCompletion("/", builtinCommands, nil)
	n := len(c.items)
	c.move(-1)
	if c.selected != n-1 {
		t.Errorf("move(-1) from 0 = %d, want %d", c.selected, n-1)
	}
	c.move(1)
	if c.selected != 0 {
		t.Errorf("move(1) wrap = %d, want 0", c.selected)
	}
}

// TestCompletionPopupLeavesTranscriptGeometryAlone is the P33.18 regression:
// the completion popup used to insert into the vertical layout and shrink
// the transcript pane by its own height every time it opened — the same
// reflow jump P33.6 fixed for the approval dialog. It now composites over
// the finished layout (renderAnchoredOverlay) instead of resizing anything
// underneath it.
func TestCompletionPopupLeavesTranscriptGeometryAlone(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	// syncCompletion() -> commandEntries() -> slash.Customs(); pre-seed the
	// customs cache so this client-less test model never calls the nil
	// daemon client (same pre-existing crash risk integration_test.go and
	// follow_intent_test.go sidestep).
	m.slash.customs = []api.CommandInfo{}

	beforeH, beforeFixed := m.transcript.Height(), m.fixedH()
	beforeChat := lipgloss.Height(m.renderChat())

	m.ta.SetValue("/")
	m.syncCompletion()
	if !m.completion.active {
		t.Fatal("expected the completion popup to be active after typing '/'")
	}

	if got := m.transcript.Height(); got != beforeH {
		t.Errorf("transcript height moved with the completion popup open: %d -> %d", beforeH, got)
	}
	if got := m.fixedH(); got != beforeFixed {
		t.Errorf("fixed vertical budget moved with the completion popup open: %d -> %d", beforeFixed, got)
	}
	if got := lipgloss.Height(m.renderChat()); got != beforeChat {
		t.Errorf("renderChat height moved with the completion popup open: %d -> %d", beforeChat, got)
	}

	full := ansi.Strip(m.renderContent())
	if !strings.Contains(full, "/help") {
		t.Errorf("expected a completion row (e.g. /help) in the composited view, got: %q", full)
	}
	if !strings.Contains(full, "AEGIS") {
		t.Errorf("expected chat content still visible behind the popup, got: %q", full)
	}
}

// TestCompletionPopupAnchorsAboveComposer checks the popup's composited
// position (P33.18): it sits directly above the composer/todo strip, not
// centered like the modal overlays (renderOverlay).
func TestCompletionPopupAnchorsAboveComposer(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.slash.customs = []api.CommandInfo{}
	m.ta.SetValue("/")
	m.syncCompletion()

	_, x, y := m.renderCompletionPopup()
	if x != 0 {
		t.Errorf("expected the popup left-anchored at x=0, got x=%d", x)
	}
	inputAreaH := m.ta.Height() + 2 + 1
	wantY := m.height - inputAreaH - completionBoxH
	if len(m.todoItems) > 0 {
		wantY -= 1
	}
	if y != wantY {
		t.Errorf("popup anchor y = %d, want %d (bottom-anchored above the composer)", y, wantY)
	}
	// The popup must not be centered like a modal overlay: a 40-row terminal
	// centers around y=~17, well above the composer.
	if y < m.height/2 {
		t.Errorf("expected popup anchored in the lower half of the screen, got y=%d height=%d", y, m.height)
	}
}
