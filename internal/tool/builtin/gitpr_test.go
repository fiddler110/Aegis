package builtin

import "testing"

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
