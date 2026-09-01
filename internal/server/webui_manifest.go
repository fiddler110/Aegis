package server

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"io/fs"
	"sort"
)

// P81.17: the committed internal/server/webui/dist/ is go:embed-ed into the
// binary with nothing verifying it against the source that produced it — the
// one CI job that did (ci.yml's "npm ci && npm run build && git diff
// --exit-code -- ../dist") runs on workflow_dispatch only, and P81.11 fixed
// that disablement as deliberate and permanent. distManifestSHA256 pins the
// digest of the reviewed bundle; checkDistDrift recomputes it from the
// embedded FS at daemon start, so a modified dist/ (whether from a bad merge
// or a deliberately planted bundle serving in the operator's browser holding
// the daemon token, P81.4) is at least observable at runtime instead of
// silent. Regenerate the pinned file after any `npm run build` with:
//
//	go run ./internal/server/webuimanifest > internal/server/webui/dist.sha256
//
//go:embed webui/dist.sha256
var distManifestSHA256 string

// hashDistFS walks fsys in sorted path order, hashing each file's path and
// content, so the result is deterministic regardless of embed.FS iteration
// order or OS path separator handling.
func hashDistFS(fsys fs.FS) (string, error) {
	var paths []string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		f, err := fsys.Open(p)
		if err != nil {
			return "", err
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", err
		}
		_ = f.Close()
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeDistDigest exposes hashDistFS for the manifest-regeneration command
// (cmd/webuimanifest), the only external caller.
func ComputeDistDigest() (string, error) {
	return hashDistFS(webUIAssets)
}

// checkDistDrift compares the embedded dist/ bundle's digest against the
// pinned manifest. driftDetected is false only when the manifest is present
// and matches — an empty/missing manifest is reported as drift rather than
// silently skipped, so a manifest that was never generated doesn't read as a
// clean bill of health.
func checkDistDrift() (driftDetected bool, actual string, err error) {
	actual, err = hashDistFS(webUIAssets)
	if err != nil {
		return true, "", err
	}
	pinned := string(bytes.TrimSpace([]byte(distManifestSHA256)))
	return pinned == "" || pinned != actual, actual, nil
}
