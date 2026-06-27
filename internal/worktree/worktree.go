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
func (m *Manager) Add(name, branch string) (string, error) {
	if !validName.MatchString(name) {
		return "", fmt.Errorf("invalid worktree name %q (use letters, digits, '.', '_', '-')", name)
	}
	dest := filepath.Join(m.base(), name)
	args := []string{"worktree", "add"}
	if branch != "" {
		if !validName.MatchString(branch) {
			return "", fmt.Errorf("invalid branch name %q", branch)
		}
		args = append(args, "-b", branch, dest)
	} else {
		args = append(args, dest)
	}
	if out, err := git(m.repoRoot, args...); err != nil {
		return "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(out))
	}
	return dest, nil
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
