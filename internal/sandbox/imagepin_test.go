package sandbox

import (
	"context"
	"path/filepath"
	"testing"
)

// withSandboxPinStore redirects the image-pin store into a scratch directory
// for the duration of a test, so tests never touch the real developer
// machine's ~/.config/aegis/sandbox_image_pins.json.
func withSandboxPinStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := imagePinStorePathFunc
	imagePinStorePathFunc = func() string { return filepath.Join(dir, "sandbox_image_pins.json") }
	t.Cleanup(func() { imagePinStorePathFunc = orig })
}

// TestPinAndVerifySandboxImageFirstUsePins is the P81.13 regression: sandbox.
// image is a mutable tag with no build-time record, so the first successful
// construction must record the image's current ID rather than refuse it.
func TestPinAndVerifySandboxImageFirstUsePins(t *testing.T) {
	withSandboxPinStore(t)
	orig := sandboxInspectImageID
	sandboxInspectImageID = func(context.Context, ContainerRuntime, string) (string, error) {
		return "sha256:aaaa", nil
	}
	t.Cleanup(func() { sandboxInspectImageID = orig })

	if err := pinAndVerifySandboxImage(context.Background(), RuntimeDocker, "ubuntu:22.04"); err != nil {
		t.Fatalf("first use: %v", err)
	}
	store, err := loadImagePinStore()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Pins["docker|ubuntu:22.04"]; got != "aaaa" {
		t.Errorf("pin = %q, want %q", got, "aaaa")
	}
}

// TestPinAndVerifySandboxImageRefusesDrift covers the refusal: once pinned, a
// later inspect returning a different ID (rebuild, retag, re-pull of the same
// tag) must be refused rather than silently trusted.
func TestPinAndVerifySandboxImageRefusesDrift(t *testing.T) {
	withSandboxPinStore(t)
	id := "sha256:aaaa"
	orig := sandboxInspectImageID
	sandboxInspectImageID = func(context.Context, ContainerRuntime, string) (string, error) {
		return id, nil
	}
	t.Cleanup(func() { sandboxInspectImageID = orig })

	if err := pinAndVerifySandboxImage(context.Background(), RuntimeDocker, "ubuntu:22.04"); err != nil {
		t.Fatalf("first use: %v", err)
	}

	id = "sha256:bbbb"
	err := pinAndVerifySandboxImage(context.Background(), RuntimeDocker, "ubuntu:22.04")
	if err == nil {
		t.Fatal("expected a refusal after the image ID changed")
	}
}

// TestPinAndVerifySandboxImageMatchingIDPasses is the happy path once pinned.
func TestPinAndVerifySandboxImageMatchingIDPasses(t *testing.T) {
	withSandboxPinStore(t)
	orig := sandboxInspectImageID
	sandboxInspectImageID = func(context.Context, ContainerRuntime, string) (string, error) {
		return "sha256:cccc", nil
	}
	t.Cleanup(func() { sandboxInspectImageID = orig })

	if err := pinAndVerifySandboxImage(context.Background(), RuntimeDocker, "ubuntu:22.04"); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := pinAndVerifySandboxImage(context.Background(), RuntimeDocker, "ubuntu:22.04"); err != nil {
		t.Errorf("second use with a matching ID should pass, got: %v", err)
	}
}
