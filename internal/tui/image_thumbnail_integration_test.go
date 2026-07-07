package tui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/provider"
)

// writeFixturePNG writes a solid-color w×h PNG to path, for tests that need
// a real decodable image file on disk.
func writeFixturePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
}

// TestSendUserMessageRendersImageThumbnail exercises the live-send path
// (P16.9): attaching an @image: token should append a rendered half-block
// thumbnail item to the transcript, sized to fit the fixed thumbnail box.
// imageProto is set directly rather than relying on config/env detection so
// the test is deterministic regardless of the host terminal.
func TestSendUserMessageRendersImageThumbnail(t *testing.T) {
	work := t.TempDir()
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: work})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.imageProto = protocolHalfBlock

	imgPath := filepath.Join(work, "shot.png")
	writeFixturePNG(t, imgPath, 40, 40)

	before := m.transcript.Len()
	m.sendUserMessage(`describe this @image:"` + imgPath + `"`)

	if m.transcript.Len() <= before+1 {
		t.Fatalf("expected at least a user-turn item plus a thumbnail item, got %d new items", m.transcript.Len()-before)
	}
	got := ansi.Strip(m.transcript.View())
	if strings.Contains(got, "@image:") {
		t.Errorf("expected the @image: token to be stripped from displayed text, got:\n%s", got)
	}
	// Find the thumbnail item directly rather than relying on plain-text
	// View() (ansi.Strip removes the SGR-styled half blocks along with
	// everything else).
	var found bool
	for _, it := range m.transcript.items {
		if it.noWrap && strings.Contains(it.raw, "\x1b[38;2;") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a noWrap thumbnail item with half-block styling in the transcript")
	}
}

// TestSendUserMessageSkipsThumbnailWhenProtocolNone confirms that when the
// terminal capability check fails (protocolNone), attaching an image still
// works exactly as it did before P16.9 — text-only, no thumbnail item, no
// error.
func TestSendUserMessageSkipsThumbnailWhenProtocolNone(t *testing.T) {
	work := t.TempDir()
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: work})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.imageProto = protocolNone

	imgPath := filepath.Join(work, "shot.png")
	writeFixturePNG(t, imgPath, 40, 40)

	m.sendUserMessage(`describe this @image:"` + imgPath + `"`)

	for _, it := range m.transcript.items {
		if it.noWrap {
			t.Errorf("expected no thumbnail item when imageProto is protocolNone, found raw=%q", it.raw)
		}
	}
}

// TestRenderImageThumbnailsFromBlocksSkipsBadData confirms loadHistory's
// image path degrades gracefully (no panic, no thumbnail) for an
// undecodable ImageBlock, e.g. corrupted or unsupported-format base64 data.
func TestRenderImageThumbnailsFromBlocksSkipsBadData(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: t.TempDir()})
	m.imageProto = protocolHalfBlock

	out := m.renderImageThumbnailsFromBlocks([]provider.ImageBlock{{Data: "not-valid-base64!!"}})
	if out != nil {
		t.Errorf("expected nil for undecodable image data, got %v", out)
	}
}

func TestRenderImageThumbnailsSkipsUnreadablePath(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: t.TempDir()})
	m.imageProto = protocolHalfBlock

	out := m.renderImageThumbnails([]api.ImageInput{{Path: filepath.Join(t.TempDir(), "missing.png")}})
	if out != nil {
		t.Errorf("expected nil for an unreadable path, got %v", out)
	}
}
