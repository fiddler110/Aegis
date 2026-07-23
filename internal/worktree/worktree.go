// Package worktree provides git-worktree management so a session can run in an
// isolated working tree — the 2026-standard mechanism for safe parallel agent
// execution. Each worktree is a separate checkout sharing the repo's object
// store; run a separate Aegis instance inside one for an isolated session.
//
// Known pitfalls (documented for callers): worktrees can collide on ports,
// databases, and caches if those are absolute/shared, and they accumulate disk
// usage quickly — prune and remove them when done.
package worktree

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const gitTimeout = 30 * time.Second

// validName restricts worktree names to a single safe path segment.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Manager manages worktrees for one repository.
type Manager struct{ repoRoot string }

// Worktree describes one linked working tree.
type Worktree struct {
	Path   string
	Head   string
	Branch string // empty when detached
}

// NewManager resolves the repository containing dir and returns a manager for it.
func NewManager(dir string) (*Manager, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	return &Manager{repoRoot: strings.TrimSpace(out)}, nil
}

// RepoRoot returns the repository's top-level directory.
func (m *Manager) RepoRoot() string { return m.repoRoot }

// base is the directory under which managed worktrees are created.
func (m *Manager) base() string { return filepath.Join(m.repoRoot, ".aegis", "worktrees") }

// Add creates a worktree named name. When branch is non-empty a new branch of
// that name is created; otherwise git creates a branch named after the
// worktree. Returns the absolute worktree path.
//
// git worktree add only checks out the committed tree, so any staged,
// unstaged, or untracked (non-ignored) changes in the source working tree
// would otherwise be invisible to the new worktree — silently dropping the
// caller's in-progress work. Add therefore mirrors the source working tree's
// dirty state onto the fresh checkout after creating it (see carryDirty): a
// copy-on-top pass that leaves the standard `git worktree add` semantics (and
// its interaction with -b) untouched. gitignored files are not carried, since
// `git status --porcelain` omits them by default.
func (m *Manager) Add(name, branch string) (string, error) {
	path, _, err := m.AddCarry(name, branch)
	return path, err
}

// AddCarry is Add, additionally returning the source-tree paths carried into
// the new worktree (dirty tracked files plus untracked non-ignored files, and
// deletions applied). Callers can report these so the carry is discoverable.
func (m *Manager) AddCarry(name, branch string) (string, []string, error) {
	if !validName.MatchString(name) {
		return "", nil, fmt.Errorf("invalid worktree name %q (use letters, digits, '.', '_', '-')", name)
	}
	dest := filepath.Join(m.base(), name)
	args := []string{"worktree", "add"}
	if branch != "" {
		if !validName.MatchString(branch) {
			return "", nil, fmt.Errorf("invalid branch name %q", branch)
		}
		args = append(args, "-b", branch, dest)
	} else {
		args = append(args, dest)
	}
	if out, err := git(m.repoRoot, args...); err != nil {
		return "", nil, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(out))
	}
	carried, err := m.carryDirty(dest)
	if err != nil {
		return dest, carried, fmt.Errorf("worktree created at %s but carrying uncommitted changes failed: %w", dest, err)
	}
	return dest, carried, nil
}

// carryDirty mirrors the source working tree's dirty state onto dest, a freshly
// created worktree that currently holds only the committed checkout. It copies
// modified/staged/untracked (non-ignored) files and applies deletions/renames
// so dest faithfully reflects the source working tree. Returns the affected
// repo-relative paths. gitignored files are excluded because
// `git status --porcelain` does not list them.
func (m *Manager) carryDirty(dest string) ([]string, error) {
	// -z: NUL-terminated records with verbatim (unquoted) paths, so paths with
	// spaces, quotes, or non-ASCII bytes are handled without unescaping.
	out, err := git(m.repoRoot, "status", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(out))
	}
	paths := parsePorcelainZ(out)
	var carried []string
	for _, rel := range paths {
		src := filepath.Join(m.repoRoot, filepath.FromSlash(rel))
		dst := filepath.Join(dest, filepath.FromSlash(rel))
		info, statErr := os.Lstat(src)
		if os.IsNotExist(statErr) {
			// Deleted (or the old name of a rename) in the source working tree:
			// drop it from the committed checkout so dest matches the source.
			if err := os.RemoveAll(dst); err != nil {
				return carried, fmt.Errorf("remove %s: %w", rel, err)
			}
			carried = append(carried, rel)
			continue
		}
		if statErr != nil {
			return carried, fmt.Errorf("stat %s: %w", rel, statErr)
		}
		if info.IsDir() {
			// Untracked whole directories are reported without a trailing entry
			// per file; nothing tracked lives here, so skip — git status lists
			// individual files for tracked changes.
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := copySymlink(src, dst); err != nil {
				return carried, err
			}
			carried = append(carried, rel)
			continue
		}
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			return carried, err
		}
		carried = append(carried, rel)
	}
	return carried, nil
}

// parsePorcelainZ extracts affected paths from `git status --porcelain -z`
// output. Each record is a 2-char status, a space, then the path; rename/copy
// records carry a second NUL-separated original path. Both the new and the old
// path of a rename are returned so the old name can be removed from dest.
func parsePorcelainZ(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if len(rec) < 4 {
			// Trailing empty field after the final NUL, or malformed — skip.
			continue
		}
		x, y := rec[0], rec[1]
		path := rec[3:] // skip "XY "
		paths = append(paths, path)
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			// The next field is the original path (rename/copy source).
			if i+1 < len(fields) {
				if orig := fields[i+1]; orig != "" {
					paths = append(paths, orig)
				}
				i++
			}
		}
	}
	return paths
}

// copyFile copies src to dst, creating parent directories and applying perm.
func copyFile(src, dst string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile honors perm only when creating; ensure mode on pre-existing files
	// (a committed checkout already put the tracked file there).
	return os.Chmod(dst, perm)
}

// copySymlink recreates the symlink src at dst.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, dst)
}

// List returns the repository's worktrees (including the main one).
func (m *Manager) List() ([]Worktree, error) {
	out, err := git(m.repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out), nil
}

// Remove deletes a managed worktree by name. force discards uncommitted changes.
func (m *Manager) Remove(name string, force bool) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	dest := filepath.Join(m.base(), name)
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, dest)
	if out, err := git(m.repoRoot, args...); err != nil {
		return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Prune removes administrative files for worktrees whose directories are gone.
func (m *Manager) Prune() error {
	if _, err := git(m.repoRoot, "worktree", "prune"); err != nil {
		return err
	}
	return nil
}

func parseWorktreeList(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			list = append(list, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return list
}

// git runs a git subcommand in dir with a bounded timeout.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
