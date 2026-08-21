package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCopyToClipboardCmdUsesOSC52 confirms P74.20: a payload within the size
// limit is sent via OSC 52 (tea.SetClipboard), not shelled out to a native
// tool — the OSC 52 path is what keeps /copy working over SSH, where the
// native tools only ever reach the clipboard of the machine Aegis runs on.
func TestCopyToClipboardCmdUsesOSC52(t *testing.T) {
	const text = "hello from P74.20"
	cmd := copyToClipboardCmd(text)
	if cmd == nil {
		t.Fatal("copyToClipboardCmd returned nil for non-empty text")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg for a payload under maxOSC52Payload, got %T", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 batched commands, got %d", len(batch))
	}

	var sawOSC52, sawResult bool
	for _, c := range batch {
		switch m := c().(type) {
		case clipboardResultMsg:
			if m.err != nil {
				t.Errorf("expected the OSC 52 path to report success, got err %v", m.err)
			}
			sawResult = true
		default:
			// setClipboardMsg is unexported in charm.land/bubbletea/v2; identify
			// it structurally (a string-kinded type carrying the copied text)
			// rather than by name.
			v := reflect.ValueOf(m)
			if v.Kind() == reflect.String && v.String() == text {
				sawOSC52 = true
			} else {
				t.Errorf("unexpected batched message type %T", m)
			}
		}
	}
	if !sawOSC52 {
		t.Error("expected one batched command to carry an OSC 52 set-clipboard message")
	}
	if !sawResult {
		t.Error("expected one batched command to report the clipboard result")
	}
}

// TestCopyToClipboardCmdFallsBackAboveOSC52Limit confirms a payload too large
// for OSC 52 skips straight to the native-tool fallback instead of emitting a
// sequence many terminals (tmux in particular) would silently truncate.
func TestCopyToClipboardCmdFallsBackAboveOSC52Limit(t *testing.T) {
	text := strings.Repeat("x", maxOSC52Payload+1)
	cmd := copyToClipboardCmd(text)
	if cmd == nil {
		t.Fatal("copyToClipboardCmd returned nil for non-empty text")
	}
	msg := cmd()
	if _, ok := msg.(tea.BatchMsg); ok {
		t.Fatal("expected the native-tool fallback (not an OSC 52 batch) above maxOSC52Payload")
	}
	if _, ok := msg.(clipboardResultMsg); !ok {
		t.Fatalf("expected clipboardResultMsg from the native-tool fallback, got %T", msg)
	}
}
