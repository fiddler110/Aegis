package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/provider"
)

// tiny 1x1 PNG.
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestBuildImageBlocksFromPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(p, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	blocks, err := buildImageBlocks([]api.ImageInput{{Path: p}})
	if err != nil {
		t.Fatalf("buildImageBlocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	ib, ok := blocks[0].(provider.ImageBlock)
	if !ok {
		t.Fatalf("block = %T, want ImageBlock", blocks[0])
	}
	if ib.MediaType != "image/png" {
		t.Errorf("media type = %q, want image/png", ib.MediaType)
	}
	if ib.Data != base64.StdEncoding.EncodeToString(pngBytes) {
		t.Error("encoded data mismatch")
	}
}

func TestBuildImageBlocksFromData(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(pngBytes)
	blocks, err := buildImageBlocks([]api.ImageInput{{MediaType: "image/png", Data: data}})
	if err != nil {
		t.Fatalf("buildImageBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].(provider.ImageBlock).Data != data {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestBuildImageBlocksRejectsUnsupported(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildImageBlocks([]api.ImageInput{{Path: p}}); err == nil {
		t.Fatal("expected error for unsupported media type")
	}
}

func TestBuildImageBlocksTooLarge(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(make([]byte, maxImageBytes+1))
	_, err := buildImageBlocks([]api.ImageInput{{MediaType: "image/png", Data: data}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}
