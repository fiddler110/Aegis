package builtin

import (
	"bytes"
	"context"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/checkpoint"
)

// shellCheckpointTimeout bounds each git subprocess used for shell-write
// checkpoint capture below, so a hung or unexpectedly slow git invocation
// can't stall the shell tool call itself — capture is best-effort and never
// worth blocking the command over.
const shellCheckpointTimeout = 5 * time.Second

// maxShellCaptureEntries caps the number of individual file paths captured
// per shell call, bounding the cost of a pathological case (a command that
// creates one huge new untracked directory) to a fixed amount of work rather
// than however many files happen to appear.
const maxShellCaptureEntries = 500

// captureShellWrites extends checkpoint/rewind coverage to files a shell
// subprocess writes directly — write_file/edit_file call Snapshotter.Capture
// themselves (see file.go), but a script run via the shell tool (e.g. a
// skill's bundled scaffold/codegen step) mutates files with no such hook, so
// without this those writes are invisible to `/rewind`: it can't restore or
// delete what it never captured a pre-image of.
//
// It works only inside a git working tree — checked once via `git rev-parse
// --is-inside-work-tree` — because a durable pre-image for a file that was
// already clean before the command comes from `git show HEAD:<path>`; outside
// a git repo there is nowhere else to recover it from once the command has
// already overwritten it, so capture is skipped entirely (best-effort,
// matching the existing best-effort git-SHA capture in server/sessions.go).
//
// Two `git status --porcelain` snapshots bracket run(): paths already
// dirty/untracked *before* the command are captured the normal way (Capture
// reads current, pre-command disk bytes — cheap, bounded to the actually
// dirty set, not a full-tree read). Paths that turn up dirty/untracked only
// *after* the command are the ones the shell write actually touched; for
// those, a pre-image is fetched from `git show HEAD:<path>` when the file was
// tracked and clean beforehand, or recorded as newly-created (rewind deletes
// it) when git has no HEAD copy — mirroring Capture's own not-found branch,
// but computed from the bracketing status diff since the file already holds
// its *post*-command content by the time this code can look at it.
func captureShellWrites(ctx context.Context, snap *checkpoint.Snapshotter, root string, run func() (string, error)) (string, error) {
	if snap == nil || !isGitWorkTree(ctx, root) {
		return run()
	}

	budget := maxShellCaptureEntries
	pre := gitStatusPaths(ctx, root)
	for rel := range pre {
		for _, f := range expandGitStatusEntry(root, rel) {
			if budget <= 0 {
				break
			}
			snap.Capture(filepath.Join(root, f))
			budget--
		}
	}

	out, err := run()

	post := gitStatusPaths(ctx, root)
	for rel := range post {
		if pre[rel] {
			continue
		}
		for _, f := range expandGitStatusEntry(root, rel) {
			if budget <= 0 {
				break
			}
			abs := filepath.Join(root, f)
			if data, ok := gitHeadContent(ctx, root, f); ok {
				snap.CaptureBytes(abs, true, data)
			} else {
				snap.CaptureBytes(abs, false, nil)
			}
			budget--
		}
	}
	return out, err
}

// isGitWorkTree reports whether root is inside a git working tree. Best
// effort: any failure (git missing, not a repo) reports false.
func isGitWorkTree(ctx context.Context, root string) bool {
	out, err := checkpointGit(ctx, root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// gitStatusPaths returns the set of workspace-relative paths git considers
// modified, staged, or untracked right now (`git status --porcelain`, git's
// own default untracked-directory collapsing left in place — expanding a
// whole untracked directory is expandGitStatusEntry's job, done only for
// entries the pre/post diff actually needs, not unconditionally here, so an
// unrelated pre-existing untracked directory never forces a full walk).
// Best-effort: a git failure yields an empty set rather than an error, so a
// transient git problem degrades to "capture nothing" rather than failing
// the shell command itself.
func gitStatusPaths(ctx context.Context, root string) map[string]bool {
	out, err := checkpointGit(ctx, root, "status", "--porcelain")
	paths := make(map[string]bool)
	if err != nil {
		return paths
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		rel := strings.TrimSpace(line[3:])
		// Rename entries read "old -> new"; track the new path (the one that
		// exists on disk now).
		if idx := strings.Index(rel, " -> "); idx >= 0 {
			rel = rel[idx+4:]
		}
		rel = strings.Trim(rel, `"`)
		if rel != "" {
			paths[rel] = true
		}
	}
	return paths
}

// expandGitStatusEntry turns one git-status path into the individual file
// paths it covers: itself, unless it names a whole untracked directory (git
// collapses an entirely-untracked directory to one "dirname/" entry), in
// which case it's walked and every file inside is returned. The walk is
// implicitly bounded by captureShellWrites' shared entry budget, which the
// caller enforces as it consumes this slice.
func expandGitStatusEntry(root, rel string) []string {
	if !strings.HasSuffix(rel, "/") {
		return []string{rel}
	}
	var out []string
	_ = filepath.WalkDir(filepath.Join(root, rel), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if r, rerr := filepath.Rel(root, path); rerr == nil {
			out = append(out, filepath.ToSlash(r))
		}
		if len(out) >= maxShellCaptureEntries {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

// gitHeadContent returns rel's content at HEAD, or (nil, false) if it isn't
// tracked there (a new file, or a repo with no commits yet).
func gitHeadContent(ctx context.Context, root, rel string) ([]byte, bool) {
	cctx, cancel := context.WithTimeout(ctx, shellCheckpointTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", root, "show", "HEAD:"+filepath.ToSlash(rel))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// checkpointGit runs a git subcommand in root under shellCheckpointTimeout
// and returns raw stdout, untrimmed — trimming is left to each caller, since
// `git status --porcelain`'s first line legitimately starts with a
// significant leading space (the staged-status column) that a whole-output
// TrimSpace would silently eat, shifting every column-based slice on that
// line by one. A distinct helper from git.go's runGit (used by the git_*
// tools): that one uses CombinedOutput and truncates at maxGitOutput, both
// wrong here — gitHeadContent needs exact, untruncated file bytes with
// stderr kept separate from stdout.
func checkpointGit(ctx context.Context, root string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, shellCheckpointTimeout)
	defer cancel()
	full := append([]string{"-C", root}, args...)
	out, err := exec.CommandContext(cctx, "git", full...).Output()
	return string(out), err
}
