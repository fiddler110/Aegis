package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/tool"
)

const (
	gitTimeout = 30 * time.Second
	// maxGitOutput bounds what one git invocation returns to the model.
	// P64.3: was 100 << 10 (100 KiB ≈ 25,600 estimated tokens), which is still
	// larger than a 4k–16k local window, so it could not bind first either.
	// 32 KiB is ~8,192 tokens — deliberately larger than shell's 24 KiB and not
	// homogenised with it, because a diff is the one over-cap result a model
	// routinely has to read in full to act on, and it has no `background:true`
	// escape hatch of its own.
	maxGitOutput = 32 << 10
	// maxTestOutput bounds the pre-commit test command's output. It is shell
	// output, not git output, so it takes shell's value and shell's end (tail).
	maxTestOutput = 24 << 10
	maxGitArgs    = 64
	maxCommitMsg  = 8 << 10 // 8 KiB
	maxCommitPath = 50
)

// readGitSubcommands is the allowlist of inspection subcommands the read-only
// `git` tool may run. Anything that mutates the repository or working tree is
// excluded and must go through `git_commit` (or the shell, with approval).
var readGitSubcommands = map[string]bool{
	"status":    true,
	"diff":      true,
	"log":       true,
	"show":      true,
	"branch":    true, // listing; flags that create/delete are rejected below
	"remote":    true,
	"blame":     true,
	"ls-files":  true,
	"shortlog":  true,
	"tag":       true, // listing
	"describe":  true,
	"rev-parse": true,
	"stash":     true, // only "stash list" is permitted (checked below)
}

// deniedGitArgPrefixes are option tokens that can escape the workspace, write
// files, or invoke external programs even from otherwise read-only
// subcommands. They are rejected wherever they appear in the argument list.
var deniedGitArgPrefixes = []string{
	"-c", "-C", "--exec-path", "--git-dir", "--work-tree",
	"--output", "-o", "--upload-pack", "--ext-diff", "--open-files-in-pager",
}

func validateGitArgs(args []string) error {
	if len(args) > maxGitArgs {
		return fmt.Errorf("too many arguments (%d, max %d)", len(args), maxGitArgs)
	}
	for _, a := range args {
		for _, bad := range deniedGitArgPrefixes {
			if a == bad || strings.HasPrefix(a, bad+"=") {
				return fmt.Errorf("argument %q is not allowed", a)
			}
		}
	}
	return nil
}

// runGit executes git with an argument vector (never a shell string, so model
// input cannot be interpreted as shell) in the workspace directory.
func runGit(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	text := string(out)
	// Head: a diff, a log or a status is read top-down and the model narrows by
	// path from what it sees first. P64.3 posture table in truncate.go.
	text = SpillHead(ctx, root, "git", text, maxGitOutput, "narrow the command with a pathspec (e.g. git diff -- <dir>) to see the rest")
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return text, fmt.Errorf("git timed out after %s", gitTimeout)
		}
		// Surface git's own message (in text) along with the exit error.
		return text, err
	}
	return text, nil
}

// --- read-only inspection ---

type gitTool struct{ root string }

func (t *gitTool) Name() string                { return "git" }
func (t *gitTool) Capability() tool.Capability { return tool.CapRead }
func (t *gitTool) Description() string {
	return "Inspect the workspace git repository (read-only): status, diff, log, show, branch, remote, blame, " +
		"ls-files, shortlog, tag, describe, rev-parse, and `stash list`. Provide a subcommand and optional args " +
		"(e.g. subcommand \"log\", args [\"--oneline\",\"-10\"]). To create commits use the git_commit tool."
}
func (t *gitTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"subcommand":{"type":"string","description":"a read-only git subcommand, e.g. status, diff, log, show, branch"},"args":{"type":"array","items":{"type":"string"},"description":"additional arguments passed to git after the subcommand"}},"required":["subcommand"]}`)
}

func (t *gitTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Subcommand string   `json:"subcommand"`
		Args       []string `json:"args"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	sub := strings.TrimSpace(args.Subcommand)
	if !readGitSubcommands[sub] {
		return tool.Result{Content: fmt.Sprintf("subcommand %q is not allowed here. Allowed: status, diff, log, show, branch, remote, blame, ls-files, shortlog, tag, describe, rev-parse, stash list.", sub), IsError: true}, nil
	}
	if err := validateGitArgs(args.Args); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, nil
	}
	// Guard the mutating shapes of otherwise-allowed subcommands.
	if reason := rejectMutatingReadArgs(sub, args.Args); reason != "" {
		return tool.Result{Content: reason, IsError: true}, nil
	}

	out, err := runGit(ctx, effectiveRoot(ctx, t.root), append([]string{sub}, args.Args...)...)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("git %s failed: %v\n%s", sub, err, out), IsError: true}, nil
	}
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}
	return tool.Result{Content: out}, nil
}

// rejectMutatingReadArgs blocks flags that would mutate state on subcommands the
// read tool otherwise permits (branch -d, tag -d, stash pop/drop, etc.).
func rejectMutatingReadArgs(sub string, args []string) string {
	switch sub {
	case "branch":
		for _, a := range args {
			switch a {
			case "-d", "-D", "--delete", "-m", "-M", "--move", "-c", "--copy", "-f", "--force":
				return "git branch: creating, deleting, moving, or renaming branches is not allowed via the git tool; use git_commit for commits or the shell for branch management"
			}
		}
	case "tag":
		for _, a := range args {
			if a == "-d" || a == "--delete" || a == "-a" || a == "--annotate" || a == "-s" {
				return "git tag: creating or deleting tags is not allowed via the git tool"
			}
		}
	case "remote":
		// "remote" is allowlisted for listing (bare, -v, show, get-url) but
		// git also treats it as a config-mutation and network-fetch verb:
		// "remote add/set-url" writes an arbitrary URL into .git/config, and
		// "remote show/update/prune" then contacts that URL — a file:// URL
		// can walk to any git repo readable by the daemon process (reachable
		// afterwards via the allowed log/show subcommands), and any URL is a
		// network egress path this CapRead tool has no business opening.
		// Block every subverb that adds/rewrites a remote's target or that
		// performs the network operation past a newly-added one.
		if len(args) > 0 {
			switch args[0] {
			case "add", "set-url", "set-branches", "set-head", "rename", "remove", "rm", "update", "prune":
				return "git remote: adding, rewriting, or fetching remotes is not allowed via the git tool; only listing (remote, remote -v, remote show <existing>, remote get-url) is permitted"
			}
		}
	case "stash":
		// Only "stash list" (and "stash show") are read-only.
		if len(args) == 0 {
			return "git stash with no subcommand would create a stash; use `stash list` to inspect"
		}
		switch args[0] {
		case "list", "show":
		default:
			return "git stash: only `stash list` and `stash show` are permitted here"
		}
	}
	return ""
}

// --- staging + commit ---

type gitCommitTool struct {
	root string
	// preCommitTest, when non-empty, is a shell command run in the workspace
	// before every commit (P46.2); a non-zero exit aborts the commit. It is
	// operator-configured (git.pre_commit_test_command) and frozen from
	// untrusted project config by the workspace-trust gate, so running it on
	// the host — the same place runGit runs git — is intentional.
	preCommitTest        string
	preCommitTestTimeout time.Duration
}

// runPreCommitTest runs the configured pre-commit test command on the host in
// root via the platform shell, returning its combined output and whether it
// passed (exit 0). A timeout or non-zero exit reports passed=false.
func runPreCommitTest(ctx context.Context, root, command string, timeout time.Duration) (out string, passed bool) {
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultPreCommitTestTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	name, args := preCommitShell(command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	b, err := cmd.CombinedOutput()
	text := string(b)
	// Tail, unlike runGit's head above, and the difference is the point of
	// P64.3's convention: this is a *test run*, not a diff. The failures are
	// the last thing printed and the head is a list of packages that passed.
	text = SpillTail(ctx, root, "pre-commit-test", text, maxTestOutput, "")
	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(text) + fmt.Sprintf("\n(pre-commit test timed out after %s)", timeout), false
	}
	return strings.TrimSpace(text), err == nil
}

// preCommitShell returns the platform shell binary and argument vector for
// running command, mirroring the sandbox package's own shell selection so the
// pre-commit gate behaves the same as the shell tool on each OS.
func preCommitShell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return sandbox.WindowsShellBinary(), []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func (t *gitCommitTool) Name() string                { return "git_commit" }
func (t *gitCommitTool) Capability() tool.Capability { return tool.CapWrite }
func (t *gitCommitTool) Description() string {
	return "Stage changes and create a git commit in the workspace. Provide a commit message. By default all " +
		"tracked modifications are staged; pass specific paths to stage only those, or all=false to commit only " +
		"what is already staged. Returns the new commit's short hash and a diffstat."
}
func (t *gitCommitTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"message":{"type":"string","description":"the commit message"},"all":{"type":"boolean","description":"stage all tracked modifications before committing (default true when no paths given)"},"paths":{"type":"array","items":{"type":"string"},"description":"specific workspace-relative paths to stage and commit"}},"required":["message"]}`)
}

func (t *gitCommitTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Message string   `json:"message"`
		All     *bool    `json:"all"`
		Paths   []string `json:"paths"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	msg := strings.TrimSpace(args.Message)
	if msg == "" {
		return tool.Result{Content: "a commit message is required", IsError: true}, nil
	}
	if len(msg) > maxCommitMsg {
		return tool.Result{Content: "commit message is too long", IsError: true}, nil
	}
	if len(args.Paths) > maxCommitPath {
		return tool.Result{Content: fmt.Sprintf("too many paths (%d, max %d)", len(args.Paths), maxCommitPath), IsError: true}, nil
	}
	for _, p := range args.Paths {
		if strings.HasPrefix(p, "-") {
			return tool.Result{Content: fmt.Sprintf("invalid path %q", p), IsError: true}, nil
		}
	}
	root := effectiveRoot(ctx, t.root)

	// Pre-commit test gate (P46.2): a red test suite blocks the commit outright,
	// so "tests pass before every commit" is mechanically enforced rather than
	// left to prose a model can skip under context pressure. No-op when
	// unconfigured. Run before staging so a failing gate leaves the index
	// untouched.
	if t.preCommitTest != "" {
		out, passed := runPreCommitTest(ctx, root, t.preCommitTest, t.preCommitTestTimeout)
		if !passed {
			return tool.Result{Content: fmt.Sprintf("commit refused: pre-commit test command failed (%s)\n%s", t.preCommitTest, out), IsError: true}, nil
		}
	}

	// Staging.
	switch {
	case len(args.Paths) > 0:
		stageArgs := append([]string{"add", "--"}, args.Paths...)
		if out, err := runGit(ctx, root, stageArgs...); err != nil {
			return tool.Result{Content: fmt.Sprintf("git add failed: %v\n%s", err, out), IsError: true}, nil
		}
	case args.All == nil || *args.All:
		// Default: stage all tracked modifications (and new files).
		if out, err := runGit(ctx, root, "add", "-A"); err != nil {
			return tool.Result{Content: fmt.Sprintf("git add failed: %v\n%s", err, out), IsError: true}, nil
		}
	}

	// Commit. Use -- to terminate options; the message is a single arg so it is
	// never shell-interpreted.
	out, err := runGit(ctx, root, "commit", "-m", msg)
	if err != nil {
		// "nothing to commit" is a common, non-fatal outcome worth reporting
		// clearly rather than as a hard error.
		if strings.Contains(out, "nothing to commit") {
			return tool.Result{Content: "nothing to commit (working tree clean or no changes staged)", IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("git commit failed: %v\n%s", err, out), IsError: true}, nil
	}

	hash, _ := runGit(ctx, root, "rev-parse", "--short", "HEAD")
	stat, _ := runGit(ctx, root, "show", "--stat", "--oneline", "HEAD")
	return tool.Result{Content: fmt.Sprintf("committed %s\n%s", strings.TrimSpace(hash), strings.TrimSpace(stat))}, nil
}
