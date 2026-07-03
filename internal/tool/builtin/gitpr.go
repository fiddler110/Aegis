package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fiddler110/aegis/internal/tool"
)

// gitPRTool closes the autonomous-work loop (P4.8): it pushes the current branch
// and opens a pull request. It prefers the `gh` CLI when available and falls
// back to pushing plus returning a GitHub "compare" URL the user can click to
// open the PR manually (no token required).
type gitPRTool struct{ root string }

func (t *gitPRTool) Name() string                { return "git_pr" }
func (t *gitPRTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *gitPRTool) Description() string {
	return "Push the current branch to the remote and open a pull request. Uses the gh CLI when available, otherwise pushes and returns a URL to open the PR. Provide a title and body; optionally a base branch (default: repo default) and draft flag."
}
func (t *gitPRTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"title":{"type":"string","description":"PR title"},"body":{"type":"string","description":"PR description"},"base":{"type":"string","description":"base branch to merge into (default: repository default branch)"},"draft":{"type":"boolean","description":"open as a draft PR (default false)"},"push":{"type":"boolean","description":"push the branch before opening the PR (default true)"}},"required":["title"]}`)
}

func (t *gitPRTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
		Push  *bool  `json:"push"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Title) == "" {
		return tool.Result{Content: "a PR title is required", IsError: true}, nil
	}

	branch, err := runGit(ctx, t.root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not determine current branch: %v", err), IsError: true}, nil
	}
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == "HEAD" {
		return tool.Result{Content: "cannot open a PR from a detached HEAD; check out a branch first", IsError: true}, nil
	}

	var out strings.Builder
	if args.Push == nil || *args.Push {
		pushOut, err := runGit(ctx, t.root, "push", "-u", "origin", branch)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("git push failed: %v\n%s", err, pushOut), IsError: true}, nil
		}
		fmt.Fprintf(&out, "pushed %s to origin\n", branch)
	}

	if ghAvailable() {
		url, err := t.createPRviaGH(ctx, args.Title, args.Body, args.Base, args.Draft)
		if err != nil {
			return tool.Result{Content: out.String() + fmt.Sprintf("gh pr create failed: %v", err), IsError: true}, nil
		}
		fmt.Fprintf(&out, "opened PR: %s", url)
		return tool.Result{Content: out.String()}, nil
	}

	// Fallback: build a compare URL from the origin remote.
	url, err := t.compareURL(ctx, args.Base, branch)
	if err != nil {
		return tool.Result{Content: out.String() + fmt.Sprintf("gh CLI not found and could not build a PR URL: %v", err), IsError: true}, nil
	}
	fmt.Fprintf(&out, "gh CLI not found; open the PR here:\n%s", url)
	return tool.Result{Content: out.String()}, nil
}

func ghAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func (t *gitPRTool) createPRviaGH(ctx context.Context, title, body, base string, draft bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	ghArgs := []string{"pr", "create", "--title", title, "--body", body}
	if base != "" {
		ghArgs = append(ghArgs, "--base", base)
	}
	if draft {
		ghArgs = append(ghArgs, "--draft")
	}
	cmd := exec.CommandContext(ctx, "gh", ghArgs...)
	cmd.Dir = t.root
	outBytes, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(outBytes))
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}
	// gh prints the PR URL on the final line.
	if lines := strings.Fields(out); len(lines) > 0 {
		return lines[len(lines)-1], nil
	}
	return out, nil
}

// compareURL derives a GitHub compare URL from the origin remote so the user
// can open a PR without gh installed.
func (t *gitPRTool) compareURL(ctx context.Context, base, branch string) (string, error) {
	remote, err := runGit(ctx, t.root, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("no origin remote: %w", err)
	}
	owner, repo, ok := parseGitHubRemote(strings.TrimSpace(remote))
	if !ok {
		return "", fmt.Errorf("origin is not a recognizable GitHub remote: %s", strings.TrimSpace(remote))
	}
	if base == "" {
		return fmt.Sprintf("https://github.com/%s/%s/pull/new/%s", owner, repo, branch), nil
	}
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s?expand=1", owner, repo, base, branch), nil
}

// parseGitHubRemote extracts owner/repo from an https or ssh GitHub remote URL.
func parseGitHubRemote(url string) (owner, repo string, ok bool) {
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		url = strings.TrimPrefix(url, "git@github.com:")
	case strings.HasPrefix(url, "https://github.com/"):
		url = strings.TrimPrefix(url, "https://github.com/")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		url = strings.TrimPrefix(url, "ssh://git@github.com/")
	default:
		return "", "", false
	}
	parts := strings.SplitN(url, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
