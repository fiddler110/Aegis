package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// multiscannerFS carries the image definition inside the binary, so `aegis
// security build-image` works from an installed binary with no checkout —
// the same reason internal/skills embeds its built-in skills.
//
// Every file the Containerfile COPYs must be listed here: the build context is
// materialized from this FS, so a script that exists on disk but not in this
// pattern fails the build with a "no such file" that points at the context
// rather than at the missing embed.
//
//go:embed multiscanner/Containerfile multiscanner/fetch.sh multiscanner/update-db.sh
var multiscannerFS embed.FS

// MultiscannerBuildOptions configures one image build.
type MultiscannerBuildOptions struct {
	// Runtime forces a container runtime; empty auto-detects via DetectBest.
	Runtime sandbox.ContainerRuntime
	// Profile is MultiscannerProfileCore or MultiscannerProfileFull.
	Profile string
	// Image is the tag to apply; empty uses MultiscannerDefaultImage.
	Image string
	// NoCache passes --no-cache, forcing every layer to rebuild. The way to
	// refresh the baked vulnerability databases, which are otherwise cached.
	NoCache bool
}

// MultiscannerBuildResult is what a successful build recorded — everything
// `aegis security build-image` needs to write the config block.
type MultiscannerBuildResult struct {
	Runtime sandbox.ContainerRuntime
	Image   string
	ImageID string
	Profile string
	Tools   []string
	// SourceFingerprint is the hash of the embedded build context this image
	// was built from. See MultiscannerSourceFingerprint.
	SourceFingerprint string
}

// MultiscannerSourceFingerprint hashes the embedded build context — the
// Containerfile and its scripts — so a recorded fingerprint can later be
// compared against the source the running binary actually carries.
//
// It exists because the image-ID pin answers a narrower question than it
// looks like it does: it proves the image hasn't changed since it was pinned,
// not that it still matches the source it was built from. Those diverge on
// every `git pull` that touches this directory, and the divergence is silent —
// a pinned image that predated the commit adding grype to fetch.sh simply
// never contained grype, and nothing said so.
//
// Unlike the image ID, a mismatch here is *not* a fail-closed condition. A
// stale image is usually still a working image; the right response is to tell
// the operator to rebuild, not to refuse to scan with what they have.
func MultiscannerSourceFingerprint() string {
	fp, err := fingerprintEmbeddedContext(multiscannerFS, "multiscanner")
	if err != nil {
		// The FS is compiled into the binary, so a read failure here is a
		// build-time impossibility rather than a runtime condition. Returning
		// empty degrades to "unknown", which every caller already handles as
		// "don't claim drift" — the same shape as an older config with no
		// fingerprint recorded.
		return ""
	}
	return fp
}

// fingerprintEmbeddedContext is the hashing itself, taking an fs.FS so tests
// can fingerprint a synthetic context and prove content changes move the hash.
//
// Filenames are sorted (map/readdir order must not affect the result) and each
// file's name and length are folded into the hash alongside its bytes, so a
// rename or a two-file boundary shift can't produce the same digest as the
// original.
func fingerprintEmbeddedContext(fsys fs.FS, dir string) (string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return "", fmt.Errorf("read embedded build context: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		data, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		// Line endings are normalized before hashing for the same reason
		// TestUpdateDBScriptIsExecutableShell checks for CRLF: a checkout on a
		// Windows host with autocrlf on would otherwise report drift against an
		// image whose source is byte-identical in git.
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// UpdateMultiscannerDB refreshes the scanner databases in the cache volume by
// running the image's aegis-update-db script.
//
// This is the only container run in the whole security package that gets
// network access, and the only one with no workspace mounted — those two facts
// are the design. Databases aren't baked into the image (they were ~3.7GB of
// 5.8GB), so they have to be fetched at some point; doing it here keeps every
// *scan* on --network none with the workspace mounted, instead of handing
// outbound network to a container that can read the user's source.
//
// skipJavaDB drops trivy's ~1.4GB Java database for callers who never scan JVM
// code.
func UpdateMultiscannerDB(ctx context.Context, p MultiscannerPolicy, skipJavaDB bool, out io.Writer) error {
	if !p.Enabled || p.Image == "" {
		return fmt.Errorf("no multiscanner image configured — run `aegis security build-image` first")
	}
	rt, ok := detectRuntime(ctx, p.RuntimePriority())
	if !ok {
		if p.Runtime != "" {
			return fmt.Errorf("the multiscanner image was built with %s, which isn't available now — start it (on Windows with Podman: `podman machine start`)", p.Runtime)
		}
		return fmt.Errorf("no container runtime available (docker/podman)")
	}
	if reason := verifyMultiscannerImage(ctx, rt, p); reason != "" {
		return fmt.Errorf("%s", reason)
	}

	args := []string{
		"run", "--rm",
		// Network ON, deliberately and uniquely — fetching the databases is
		// the entire job. Kept safe by what's absent: no workspace mount, so
		// there is no source for a networked container to reach.
		"--network", "bridge",
		"-v", MultiscannerCacheVolume + ":" + multiscannerCacheMount,
	}
	// Inserted after the fixed flags rather than baked into the literal: wslc
	// rejects them outright, and update-db failing on a flag would read as a
	// database problem (see sandbox.OCIHardeningFlags).
	args = append(args, sandbox.OCIHardeningFlags(rt)...)
	if skipJavaDB {
		args = append(args, "-e", "AEGIS_SKIP_JAVA_DB=true")
	}
	args = append(args, p.Image, "aegis-update-db")

	cmd := exec.CommandContext(ctx, string(rt), args...)
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: database update failed: %w", rt, err)
	}
	return nil
}

// normalizeProfile validates and canonicalizes a --profile value.
func normalizeProfile(p string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(p)) {
	case "", MultiscannerProfileFull:
		return MultiscannerProfileFull, nil
	case MultiscannerProfileCore:
		return MultiscannerProfileCore, nil
	default:
		return "", fmt.Errorf("unknown profile %q (want %s)", p, strings.Join(MultiscannerProfiles(), " or "))
	}
}

// MaterializeMultiscannerContext writes the embedded build context (the
// Containerfile and its fetch script) into dir.
func MaterializeMultiscannerContext(dir string) error {
	entries, err := fs.ReadDir(multiscannerFS, "multiscanner")
	if err != nil {
		return fmt.Errorf("read embedded build context: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := multiscannerFS.ReadFile("multiscanner/" + e.Name())
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		// 0o755 on fetch.sh is belt-and-braces: the Containerfile chmods it
		// after COPY anyway, because a mode set on a Windows host doesn't
		// survive into the build context reliably.
		mode := os.FileMode(0o644)
		if strings.HasSuffix(e.Name(), ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, mode); err != nil {
			return fmt.Errorf("write %s: %w", e.Name(), err)
		}
	}
	return nil
}

// buildFromEmbeddedContext runs one image build out of an already-materialized
// build context and returns the built image's canonical ID.
//
// Shared by both images (P55.7), which is the point: they come out of the same
// Containerfile via different --target stages, so they share one fetch script,
// one set of pinned tool versions, and one source fingerprint. A second build
// function with its own copy of the ID-inspection logic would be the obvious
// place for the two to drift apart on exactly the thing that binds Aegis to an
// image.
func buildFromEmbeddedContext(ctx context.Context, rt sandbox.ContainerRuntime, dir, image, target string, noCache bool, buildArgs []string, out io.Writer) (string, error) {
	args := []string{"build"}
	for _, a := range buildArgs {
		args = append(args, "--build-arg", a)
	}
	if target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, "-t", image, "-f", filepath.Join(dir, "Containerfile"))
	if noCache {
		args = append(args, "--no-cache")
	}
	args = append(args, dir)

	cmd := exec.CommandContext(ctx, string(rt), args...)
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s build failed: %w", rt, err)
	}

	id, err := inspectImageID(ctx, rt, image)
	if err != nil {
		return "", fmt.Errorf("build reported success but the image can't be inspected: %w", err)
	}
	id = strings.TrimSpace(id)
	if normalizeImageID(id) == "" {
		return "", fmt.Errorf("%s returned an empty image ID for %s", rt, image)
	}
	// Store the canonical "sha256:<hex>" form regardless of which dialect the
	// runtime reported (podman omits the prefix, docker includes it);
	// normalizeImageID makes the comparison itself format-agnostic either way.
	if !strings.HasPrefix(id, "sha256:") {
		id = "sha256:" + id
	}
	return id, nil
}

// NetscannerBuildResult is what a successful netscanner build recorded.
type NetscannerBuildResult struct {
	Runtime sandbox.ContainerRuntime
	Image   string
	ImageID string
	Tools   []string
	// SourceFingerprint is the same build-context hash the multiscanner
	// records, because both images are built from that one context.
	SourceFingerprint string
}

// BuildNetscanner builds the network-facing image (P55.7) — the one that runs
// with network on and no workspace mounted — and returns its identity.
//
// No profile argument, deliberately. The multiscanner has profiles because its
// cost is dominated by interpreter stacks a given project may not need; this
// image is four tools with one shared posture, and a "core netscanner" would be
// a knob with nothing behind it.
func BuildNetscanner(ctx context.Context, opts MultiscannerBuildOptions, out io.Writer) (NetscannerBuildResult, error) {
	var res NetscannerBuildResult

	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = NetscannerDefaultImage
	}
	if image == MultiscannerDefaultImage {
		// Two different images under one tag would leave whichever was built
		// second answering for both pins, and the first one's ID check failing
		// with a message about a rebuild that never happened.
		return res, fmt.Errorf("%s is the multiscanner's tag — the netscanner needs its own (default %s)", image, NetscannerDefaultImage)
	}

	rt := opts.Runtime
	if rt == "" {
		detected, ok := detectRuntime(ctx, nil)
		if !ok {
			return res, fmt.Errorf("no container runtime available (looked for docker/podman) — install one, then re-run; on Windows with Podman, `podman machine start` must also have been run")
		}
		rt = detected
	}

	dir, err := os.MkdirTemp("", "aegis-netscanner-*")
	if err != nil {
		return res, fmt.Errorf("create build context: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := MaterializeMultiscannerContext(dir); err != nil {
		return res, err
	}

	id, err := buildFromEmbeddedContext(ctx, rt, dir, image, netscannerBuildTarget, opts.NoCache, nil, out)
	if err != nil {
		return res, err
	}

	return NetscannerBuildResult{
		Runtime:           rt,
		Image:             image,
		ImageID:           id,
		Tools:             NetscannerTools(),
		SourceFingerprint: MultiscannerSourceFingerprint(),
	}, nil
}

// BuildMultiscanner builds the shared scanner image and returns its identity,
// streaming the runtime's build output to out (which may be nil).
//
// The returned ImageID is the point of the whole exercise: it's what `aegis
// security build-image` records into config and what Resolve re-verifies
// before every container run. See MultiscannerConfig for why an image ID
// rather than the digest-pinned reference every other scanner image needs.
func BuildMultiscanner(ctx context.Context, opts MultiscannerBuildOptions, out io.Writer) (MultiscannerBuildResult, error) {
	var res MultiscannerBuildResult

	profile, err := normalizeProfile(opts.Profile)
	if err != nil {
		return res, err
	}
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = MultiscannerDefaultImage
	}

	rt := opts.Runtime
	if rt == "" {
		detected, ok := detectRuntime(ctx, nil)
		if !ok {
			return res, fmt.Errorf("no container runtime available (looked for docker/podman) — install one, then re-run; on Windows with Podman, `podman machine start` must also have been run")
		}
		rt = detected
	}

	dir, err := os.MkdirTemp("", "aegis-multiscanner-*")
	if err != nil {
		return res, fmt.Errorf("create build context: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := MaterializeMultiscannerContext(dir); err != nil {
		return res, err
	}

	id, err := buildFromEmbeddedContext(ctx, rt, dir, image, "", opts.NoCache, []string{"PROFILE=" + profile}, out)
	if err != nil {
		return res, err
	}

	return MultiscannerBuildResult{
		Runtime: rt,
		Image:   image,
		ImageID: id,
		Profile: profile,
		Tools:   MultiscannerTools(profile),
		// Recorded from the same embedded FS the build context was
		// materialized from a few lines up, so the fingerprint describes
		// exactly what went into this image.
		SourceFingerprint: MultiscannerSourceFingerprint(),
	}, nil
}
