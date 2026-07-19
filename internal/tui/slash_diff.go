package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

const diffTimeout = 15 * time.Second

// reviewTimeout is longer than diffTimeout: /review's git calls are the same
// as /diff's, but it also needs headroom for merge-base/rev-parse round
// trips against a large history before the model turn (which runs under its
// own timeout via startStream) even begins.
const reviewTimeout = 30 * time.Second

// maxReviewDiffChars bounds how much diff text /review inlines into the
// review prompt. Unlike /diff (rendered locally, no model involved), this
// diff becomes part of the conversation's context, so an unbounded diff
// (e.g. reviewing against a distant branch) could blow the context budget
// before the model does anything useful with it.
const maxReviewDiffChars = 200_000

// cmdDiff shows the working-tree git diff — tracked changes (staged and
// unstaged) plus untracked files, which plain `git diff` omits — as a
// syntax-highlighted transcript block with no model turn spent (P22.1, same
// no-model-turn pattern as /scan). Runs directly against the TUI process's
// own workspace (d.workDir), consistent with /sandbox and /security-config
// rather than a daemon round trip.
//
// Usage:
//
//	/diff                  tracked changes vs HEAD, plus untracked files
//	/diff <path>            same, scoped to a workspace-relative path
//	/diff --staged [path]   only staged (index) changes; untracked files
//	                        are never staged so they're excluded here
func (d *SlashDispatcher) cmdDiff(args []string) SlashResult {
	staged := false
	var path string
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--staged", "--cached":
			staged = true
		default:
			path = a
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()

	if _, err := runGitDiff(ctx, d.workDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return SlashResult{Output: fmt.Sprintf("Not a git repository: %v", err), IsError: true}
	}

	var out strings.Builder
	trackedArgs := []string{"diff", "--cached"}
	if !staged {
		trackedArgs = []string{"diff", "HEAD"}
	}
	if path != "" {
		trackedArgs = append(trackedArgs, "--", path)
	}
	text, err := runGitDiff(ctx, d.workDir, trackedArgs...)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	out.WriteString(text)

	if !staged {
		untracked, err := untrackedFiles(ctx, d.workDir, path)
		if err != nil {
			return SlashResult{Output: err.Error(), IsError: true}
		}
		for _, f := range untracked {
			text, _ := runGitDiff(ctx, d.workDir, "diff", "--no-index", "--", "/dev/null", f)
			out.WriteString(text)
		}
	}

	if strings.TrimSpace(out.String()) == "" {
		return SlashResult{Output: "No changes."}
	}
	// \x00diff carries the raw diff text through to tui.go, which has the
	// active theme needed for chroma highlighting — the dispatcher itself
	// has no theme reference (same reason /theme and /clear use \x00
	// markers instead of pre-rendering here).
	return SlashResult{Output: "\x00diff\n" + out.String()}
}

// cmdReview implements /review (P22.2): a dedicated read-only review flow
// over a diff, composed from pieces Aegis already has — the content-review
// skill's structured severity rubric, and plan (read-only) permission mode
// enforced for the duration of the review — rather than a bespoke reviewer.
// Unlike /diff, this spends a model turn: the diff is inlined into a prompt
// that loads the content-review skill and sent as a normal message in the
// current session, so streaming/approval/cost tracking all work exactly as
// they do for any other turn.
//
// Usage:
//
//	/review                uncommitted working-tree changes (tracked + untracked)
//	/review --staged       only staged (index) changes
//	/review <branch>       diff against the merge-base with <branch>
//	/review <commit>       diff for that single commit
//
// Activates the content-review built-in skill for this session on demand
// (see activateSkill) so its structured severity-rubric format applies
// without needing it pre-enabled via config.
func (d *SlashDispatcher) cmdReview(args []string) SlashResult {
	staged := false
	var ref string
	for _, a := range args {
		if strings.EqualFold(a, "--staged") || strings.EqualFold(a, "--cached") {
			staged = true
			continue
		}
		if ref != "" {
			return SlashResult{Output: "usage: /review [--staged | <branch|commit>]", IsError: true}
		}
		ref = a
	}
	if staged && ref != "" {
		return SlashResult{Output: "usage: /review [--staged | <branch|commit>]", IsError: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()

	if _, err := runGitDiff(ctx, d.workDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return SlashResult{Output: fmt.Sprintf("Not a git repository: %v", err), IsError: true}
	}

	diffText, desc, err := reviewTargetDiff(ctx, d.workDir, staged, ref)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if strings.TrimSpace(diffText) == "" {
		return SlashResult{Output: "No changes to review."}
	}

	truncated := false
	if runes := []rune(diffText); len(runes) > maxReviewDiffChars {
		diffText = string(runes[:maxReviewDiffChars])
		truncated = true
	}

	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Load the content-review skill and review %s as a code review. Here is the diff:\n\n```diff\n%s\n```\n", desc, diffText)
	if truncated {
		prompt.WriteString("\n(diff truncated to fit — note this in your scope/method statement rather than treating it as the whole change)\n")
	}
	prompt.WriteString("\nRead whatever surrounding code, tests, or history the skill's ground-truth step calls for — don't limit yourself to the diff hunks. Report the ranked findings summary in chat per the skill's format.")

	body, warn := d.activateSkill("content-review")
	res := SlashResult{Message: skillTaskMessage("content-review", body, prompt.String()), Output: warn}
	if d.mode != "plan" {
		prevMode, newMode := d.mode, "plan"
		if _, err := d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{Mode: &newMode}); err == nil {
			d.mode = "plan"
			res.Output += fmt.Sprintf("Switched to plan mode for a read-only review (was %s) — /mode %s to switch back afterward.", prevMode, prevMode)
		}
	}
	return res
}

// reviewTargetDiff resolves /review's target into diff text plus a
// human-readable description of what's being reviewed, for cmdReview to
// inline into the review prompt.
func reviewTargetDiff(ctx context.Context, dir string, staged bool, ref string) (diffText, desc string, err error) {
	switch {
	case ref != "":
		return reviewRefDiff(ctx, dir, ref)
	case staged:
		text, err := runGitDiff(ctx, dir, "diff", "--cached")
		if err != nil {
			return "", "", err
		}
		return text, "the staged changes", nil
	default:
		text, err := runGitDiff(ctx, dir, "diff", "HEAD")
		if err != nil {
			return "", "", err
		}
		var b strings.Builder
		b.WriteString(text)
		untracked, err := untrackedFiles(ctx, dir, "")
		if err != nil {
			return "", "", err
		}
		for _, f := range untracked {
			t, _ := runGitDiff(ctx, dir, "diff", "--no-index", "--", "/dev/null", f)
			b.WriteString(t)
		}
		return b.String(), "the uncommitted working-tree changes", nil
	}
}

// reviewRefDiff resolves a single /review <ref> argument: a named branch or
// tag reviews as the diff against its merge-base with HEAD (the usual
// "what would this PR change" question); anything else that resolves to a
// commit (a SHA, HEAD~2, etc.) reviews as that one commit's own diff.
func reviewRefDiff(ctx context.Context, dir, ref string) (diffText, desc string, err error) {
	if _, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return "", "", fmt.Errorf("%q is not a valid git ref", ref)
	}

	if refIsNamed(ctx, dir, ref) {
		base, err := runGit(ctx, dir, "merge-base", ref, "HEAD")
		if err != nil {
			return "", "", fmt.Errorf("no merge base found with %q", ref)
		}
		base = strings.TrimSpace(base)
		text, err := runGitDiff(ctx, dir, "diff", base, "HEAD")
		if err != nil {
			return "", "", err
		}
		return text, fmt.Sprintf("the diff between %s and HEAD (merge-base %s)", ref, ref), nil
	}

	text, err := runGitDiff(ctx, dir, "diff", ref+"^", ref)
	if err != nil {
		// No parent to diff against (a root commit) — fall back to the
		// commit's own patch.
		text, err = runGitDiff(ctx, dir, "show", ref)
		if err != nil {
			return "", "", err
		}
	}
	return text, fmt.Sprintf("commit %s", ref), nil
}

// refIsNamed reports whether ref resolves to a branch or tag (local or
// remote-tracking), as opposed to a bare commit-ish like a SHA or HEAD~2.
func refIsNamed(ctx context.Context, dir, ref string) bool {
	for _, prefix := range []string{"refs/heads/", "refs/remotes/", "refs/tags/"} {
		cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", prefix+ref)
		cmd.Dir = dir
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

// runGit runs a non-diff git subcommand and returns its stdout, treating any
// non-zero exit as a real error — unlike runGitDiff, nothing here has a
// meaningful "differences found" exit code to special-case.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// runGitDiff runs a git diff-family subcommand and returns its stdout. Unlike
// most git subcommands, `git diff` / `git diff --no-index` exit 1 (not 0)
// when they find differences — only exit codes >1, or a failure to start git
// at all, are real errors.
func runGitDiff(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return stdout.String(), nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git diff failed: %s", msg)
	}
	return stdout.String(), nil
}

// untrackedFiles lists workspace-relative paths of untracked (not ignored)
// files, optionally scoped to path, so cmdDiff can synthesize a "new file"
// diff for each via `git diff --no-index`.
func untrackedFiles(ctx context.Context, dir, path string) ([]string, error) {
	args := []string{"ls-files", "--others", "--exclude-standard"}
	if path != "" {
		args = append(args, "--", path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git ls-files failed: %s", msg)
	}
	var files []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
