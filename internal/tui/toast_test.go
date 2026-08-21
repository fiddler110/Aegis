package tui

import "testing"

// TestStaleToastExpiryDoesNotRetireNewerToast is the P63.10 regression: two
// toasts shown in quick succession must not let the first one's expiry timer
// cut the second one short.
func TestStaleToastExpiryDoesNotRetireNewerToast(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})

	first, _ := newToastCmd("first", toastInfo)
	m.activeToast = first
	firstExpiry := toastExpiredMsg{t: first}

	second, _ := newToastCmd("second", toastInfo)
	m.activeToast = second

	m = m.updateToastExpired(firstExpiry)
	if m.activeToast != second {
		t.Fatalf("expected the newer toast to survive the stale expiry, got %#v", m.activeToast)
	}

	m = m.updateToastExpired(toastExpiredMsg{t: second})
	if m.activeToast != nil {
		t.Fatalf("expected the current toast's own expiry to clear it, got %#v", m.activeToast)
	}
}
