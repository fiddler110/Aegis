package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/tool"
)

// maxBranchName bounds a ref name. Git's own limit is the filesystem's, but a
// name this long is a mistake or an attack rather than a branch.
const maxBranchName = 255

// gitBranchTool closes GAP-04: `git` is read-only and `git_commit` commits, so
// until now the one workflow step between them — put this work on a branch —
// had no tool at all and had to go through `shell`. That inverted the risk
// gradient the permission layer is supposed to express: the safe, routine
// operation (branch before editing) needed CapExecute and an approval for an
// arbitrary command line, while the far larger blast radius of `shell` was the
// only way to get it. This tool makes the safe operation the easy one.
//
// Capability is CapWrite, not CapExecute: switching a branch rewrites the
// working tree, so it mutates local files, but it cannot run an arbitrary
// binary. That is Deny in plan mode and Ask in build mode — strictly narrower
// than the `shell` call it replaces.
//
// Deliberately absent: merge, rebase, reset and force-delete. Each either
// rewrites history or discards committed work, and none of them is the routine
// operation this tool exists to make cheap. They stay in `shell`, where the
// approval prompt shows the operator the exact command.
type gitBranchTool struct{ root string }

func (t *gitBranchTool) Name() string                { return "git_branch" }
func (t *gitBranchTool) Capability() tool.Capability { return tool.CapWrite }

func (t *gitBranchTool) Description() string {
	return "Manage git branches in the workspace: list branches, create a branch, switch to one, or delete a " +
		"fully-merged branch. Use operation \"create\" (optionally with from, and switch=true to move onto it), " +
		"\"switch\", \"list\", or \"delete\". Merging, rebasing and force-deleting an unmerged branch are not " +
		"supported here."
}

func (t *gitBranchTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{` +
		`"operation":{"type":"string","enum":["list","create","switch","delete"],"description":"what to do: list, create, switch, or delete"},` +
		`"name":{"type":"string","description":"the branch name; required for create, switch and delete"},` +
		`"from":{"type":"string","description":"for create: the start point (branch, tag or commit) to branch from; defaults to the current HEAD"},` +
		`"switch":{"type":"boolean","description":"for create: also switch to the new branch (default false)"}` +
		`},"required":["operation"]}`)
}

// gitBranchOperations is the enforced allowlist. VULN-04 is the standing lesson
// here: the enum in InputSchema above is advisory — nothing in this package
// validates tool input against its own schema — so the operation is checked
// against this map, not against the schema the model was shown.
var gitBranchOperations = map[string]bool{
	"list":   true,
	"create": true,
	"switch": true,
	"delete": true,
}

func (t *gitBranchTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Operation string `json:"operation"`
		Name      string `json:"name"`
		From      string `json:"from"`
		Switch    bool   `json:"switch"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	op := strings.TrimSpace(args.Operation)
	if !gitBranchOperations[op] {
		return tool.Result{Content: fmt.Sprintf(
			"operation %q is not supported. Use one of: list, create, switch, delete.", args.Operation), IsError: true}, nil
	}
	root := effectiveRoot(ctx, t.root)

	if op == "list" {
		out, err := runGit(ctx, root, "branch", "--list")
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("git branch --list failed: %v\n%s", err, out), IsError: true}, nil
		}
		if strings.TrimSpace(out) == "" {
			out = "(no branches yet — the repository has no commits)"
		}
		return tool.Result{Content: out}, nil
	}

	name := strings.TrimSpace(args.Name)
	if err := validateBranchName(name); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, nil
	}

	switch op {
	case "create":
		argv := []string{"branch", name}
		if from := strings.TrimSpace(args.From); from != "" {
			if err := validateStartPoint(from); err != nil {
				return tool.Result{Content: err.Error(), IsError: true}, nil
			}
			argv = append(argv, from)
		}
		if out, err := runGit(ctx, root, argv...); err != nil {
			return tool.Result{Content: fmt.Sprintf("git branch %s failed: %v\n%s", name, err, out), IsError: true}, nil
		}
		if !args.Switch {
			return tool.Result{Content: fmt.Sprintf("created branch %s (still on the previous branch; pass switch=true or use operation \"switch\" to move onto it)", name)}, nil
		}
		out, err := runGit(ctx, root, "checkout", name)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("created branch %s but could not switch to it: %v\n%s", name, err, out), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("created and switched to branch %s\n%s", name, out)}, nil

	case "switch":
		out, err := runGit(ctx, root, "checkout", name)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("git checkout %s failed: %v\n%s", name, err, out), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("switched to branch %s\n%s", name, out)}, nil

	case "delete":
		// -d only, never -D. Git's -d refuses to delete a branch whose commits
		// are not merged, which is exactly the refusal we want to keep: this
		// tool must not be a way to discard committed work without the operator
		// seeing the command. The error text points at the escape hatch rather
		// than pretending none exists.
		out, err := runGit(ctx, root, "branch", "-d", name)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf(
				"git branch -d %s failed: %v\n%s\nIf the branch holds unmerged commits, deleting it discards them; "+
					"that has to go through the shell tool so the operator approves the exact command.",
				name, err, out), IsError: true}, nil
		}
		return tool.Result{Content: out}, nil
	}
	return tool.Result{Content: fmt.Sprintf("operation %q is not supported", op), IsError: true}, nil
}

// validateBranchName applies git's check-ref-format rules locally rather than
// spending a subprocess on `git check-ref-format`. It is deliberately stricter
// than git in one place — a leading "-" is refused before anything else — since
// that is the flag-injection shape, and argv construction alone would let
// "--force" through as a positional that git then reads as an option.
func validateBranchName(name string) error {
	if name == "" {
		return fmt.Errorf("a branch name is required")
	}
	if len(name) > maxBranchName {
		return fmt.Errorf("branch name is too long (%d characters, max %d)", len(name), maxBranchName)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid branch name %q: a name may not start with \"-\"", name)
	}
	for _, bad := range []string{"..", "@{", "//", "\\", "~", "^", ":", "?", "*", "[", " ", "\t"} {
		if strings.Contains(name, bad) {
			return fmt.Errorf("invalid branch name %q: it may not contain %q", name, bad)
		}
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid branch name %q: it contains a control character", name)
		}
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") ||
		strings.HasSuffix(name, ".lock") || name == "@" {
		return fmt.Errorf("invalid branch name %q: it may not begin or end with \"/\" or \".\", end with \".lock\", or be \"@\"", name)
	}
	return nil
}

// validateStartPoint is looser than validateBranchName because a start point is
// a revision, not a ref name: "HEAD~1" and "origin/main^" are legitimate here
// and forbidden as branch names. The parts that matter are the same — no flag
// injection, no whitespace, no control characters.
func validateStartPoint(rev string) error {
	if len(rev) > maxBranchName {
		return fmt.Errorf("start point is too long (%d characters, max %d)", len(rev), maxBranchName)
	}
	if strings.HasPrefix(rev, "-") {
		return fmt.Errorf("invalid start point %q: it may not start with \"-\"", rev)
	}
	for _, bad := range []string{"..", " ", "\t", "\\", ":", "?", "*", "["} {
		if strings.Contains(rev, bad) {
			return fmt.Errorf("invalid start point %q: it may not contain %q", rev, bad)
		}
	}
	for _, r := range rev {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid start point %q: it contains a control character", rev)
		}
	}
	return nil
}
