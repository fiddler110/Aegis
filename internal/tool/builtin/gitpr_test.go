package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/security"
)

// TestGitPRRefusesOnSecretFinding covers P24.6 / FIND-13: git_pr must not
// push or call `gh pr create` when the title/body scan turns up a finding.
// It stubs scanPRTextForSecrets rather than relying on the real gitleaks
// binary, mirroring how internal/security's own gitleaks tests exercise
// parseGitleaks directly instead of shelling out.
func TestGitPRRefusesOnSecretFinding(t *testing.T) {
	orig := scanPRTextForSecrets
	t.Cleanup(func() { scanPRTextForSecrets = orig })
	scanPRTextForSecrets = func(ctx context.Context, text string) ([]security.Finding, error) {
		if !strings.Contains(text, "supersecret title") {
			t.Fatalf("scan called with unexpected text: %q", text)
		}
		return []security.Finding{{
			Tool:     "gitleaks",
			RuleID:   "aws-key",
			Title:    "AWS Access Key",
			Location: "content.txt:1",
		}}, nil
	}

	pr := &gitPRTool{root: t.TempDir()}
	input, err := json.Marshal(map[string]string{"title": "supersecret title", "body": "body text"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := pr.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true when a secret is found, got result: %+v", res)
	}
	if !strings.Contains(res.Content, "aws-key") || !strings.Contains(res.Content, "AWS Access Key") {
		t.Errorf("result content should surface the finding's rule/title, got: %q", res.Content)
	}
	if !strings.Contains(res.Content, "refusing") {
		t.Errorf("result content should make clear the PR was refused, got: %q", res.Content)
	}
}

// TestGitPRFailsOpenOnScanError covers the "scanner malfunction must not
// block a legitimate PR" requirement: when scanPRTextForSecrets itself
// errors (as opposed to cleanly finding nothing), Execute must proceed past
// the scan rather than surface a "secret detected" refusal.
func TestGitPRFailsOpenOnScanError(t *testing.T) {
	orig := scanPRTextForSecrets
	t.Cleanup(func() { scanPRTextForSecrets = orig })
	scanPRTextForSecrets = func(ctx context.Context, text string) ([]security.Finding, error) {
		return nil, errors.New("gitleaks exploded")
	}

	// root is not a git repo, so once the scan step is passed, Execute will
	// fail later trying to resolve the current branch — that failure (not a
	// "secret detected" refusal) is what proves the scan was failed open.
	pr := &gitPRTool{root: t.TempDir()}
	input, err := json.Marshal(map[string]string{"title": "a normal title", "body": "a normal body"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := pr.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result (from the non-repo root, not the scan), got: %+v", res)
	}
	if strings.Contains(res.Content, "secret") {
		t.Errorf("scan error should fail open, not surface a secret-detected refusal; got: %q", res.Content)
	}
}

func TestParseGitHubRemote(t *testing.T) {
	cases := []struct {
		url, owner, repo string
		ok               bool
	}{
		{"git@github.com:acme/widgets.git", "acme", "widgets", true},
		{"https://github.com/acme/widgets.git", "acme", "widgets", true},
		{"https://github.com/acme/widgets", "acme", "widgets", true},
		{"ssh://git@github.com/acme/widgets.git", "acme", "widgets", true},
		{"https://gitlab.com/acme/widgets.git", "", "", false},
		{"not a url", "", "", false},
	}
	for _, c := range cases {
		owner, repo, ok := parseGitHubRemote(c.url)
		if ok != c.ok || owner != c.owner || repo != c.repo {
			t.Errorf("parseGitHubRemote(%q) = %q,%q,%v want %q,%q,%v", c.url, owner, repo, ok, c.owner, c.repo, c.ok)
		}
	}
}
