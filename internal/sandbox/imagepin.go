package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// P81.13: sandbox.image (default "ubuntu:22.04") is a mutable tag — its
// backing image can change over time, or be replaced locally, with no
// signal. Unlike the security package's locally-built multiscanner/netscanner
// images, this one is typically pulled from a registry and has no
// build-time record to pin against, so the first successful backend
// construction records the runtime's real image ID as the pin; every
// subsequent construction re-verifies it and refuses a mismatch rather than
// silently running a different image than the operator last approved.
//
// This intentionally does not live in internal/config: config already
// imports internal/sandbox, so importing config back here would cycle. The
// pin store's location is computed independently, matching config's
// defaultDataDir layout so an operator finds it in the one place they'd
// expect Aegis state to live.

var pinStoreMu sync.Mutex

type imagePinStore struct {
	// Pins maps "<runtime>|<image>" -> the image ID recorded at first use.
	Pins map[string]string `json:"pins"`
}

// sandboxDataDir mirrors internal/config's defaultDataDir without importing
// it (see the package-level doc comment for why).
func sandboxDataDir() string {
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".config", "aegis")
		}
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "aegis")
}

// imagePinStorePathFunc is a seam over the pin store's location so tests can
// redirect it into a scratch directory instead of the real developer
// machine's data dir.
var imagePinStorePathFunc = func() string {
	return filepath.Join(sandboxDataDir(), "sandbox_image_pins.json")
}

func imagePinStorePath() string {
	return imagePinStorePathFunc()
}

func loadImagePinStore() (imagePinStore, error) {
	var s imagePinStore
	b, err := os.ReadFile(imagePinStorePath())
	if err != nil {
		if os.IsNotExist(err) {
			return imagePinStore{Pins: map[string]string{}}, nil
		}
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return imagePinStore{Pins: map[string]string{}}, nil
	}
	if s.Pins == nil {
		s.Pins = map[string]string{}
	}
	return s, nil
}

func saveImagePinStore(s imagePinStore) error {
	path := imagePinStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// sandboxInspectImageID is a seam over the runtime's image inspect, mirroring
// internal/security's inspectImageID (duplicated rather than shared, to keep
// this package independent of internal/security's own dependency on
// internal/config).
var sandboxInspectImageID = func(ctx context.Context, rt ContainerRuntime, image string) (string, error) {
	args := []string{"image", "inspect", "--format", "{{.Id}}", image}
	if rt == RuntimeWSL {
		args = []string{"image", "inspect", image}
	}
	cmd := exec.CommandContext(ctx, string(rt), args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s image inspect %s: %w: %s", rt, image, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s image inspect %s: %w", rt, image, err)
	}
	if rt == RuntimeWSL {
		var records []struct {
			Id string `json:"Id"`
		}
		if jsonErr := json.Unmarshal(out, &records); jsonErr != nil || len(records) == 0 {
			return "", fmt.Errorf("%s image inspect %s: could not parse JSON output", rt, image)
		}
		return strings.TrimSpace(records[0].Id), nil
	}
	return strings.TrimSpace(string(out)), nil
}

func normalizeImageIDForPin(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.TrimPrefix(id, "sha256:")
}

// pinAndVerifySandboxImage records rt/image's current ID on first use and
// refuses on every later use where the image no longer matches — a rebuild,
// a retag, or a `docker pull` of the same tag pointing somewhere new. It
// fails open on inspect errors other than a mismatch (the image may not
// exist locally yet and will be pulled implicitly by the run that follows,
// or the runtime CLI may not support the flag) so a first-time or
// unusual-runtime setup isn't blocked by a check that exists to catch drift,
// not availability.
func pinAndVerifySandboxImage(ctx context.Context, rt ContainerRuntime, image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return nil
	}
	pinStoreMu.Lock()
	defer pinStoreMu.Unlock()

	actual, err := sandboxInspectImageID(ctx, rt, image)
	if err != nil {
		// Can't verify — don't block on it. The container run itself will
		// surface a missing/unpullable image with its own clear error.
		return nil
	}
	actual = normalizeImageIDForPin(actual)
	if actual == "" {
		return nil
	}

	store, err := loadImagePinStore()
	if err != nil {
		// A corrupt or unreadable pin store shouldn't block sandbox startup;
		// treat it as "no pin yet" and let a later successful load recover.
		return nil
	}
	key := string(rt) + "|" + image
	pinned, ok := store.Pins[key]
	if !ok {
		store.Pins[key] = actual
		_ = saveImagePinStore(store)
		return nil
	}
	if pinned != actual {
		return fmt.Errorf(
			"sandbox: image %q no longer matches the ID pinned at first use (have %s, pinned %s) — it was rebuilt, retagged, or re-pulled since Aegis last approved it. "+
				"Delete its entry from %s to accept the new image, or pin a different sandbox.image",
			image, shortSandboxImageID(actual), shortSandboxImageID(pinned), imagePinStorePath(),
		)
	}
	return nil
}

func shortSandboxImageID(id string) string {
	id = normalizeImageIDForPin(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
