package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendProjectPermissionRuleCreatesFile(t *testing.T) {
	root := t.TempDir()
	if err := AppendProjectPermissionRule(root, "allow shell(npm test*)"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".aegis", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "allow shell(npm test*)") {
		t.Fatalf("rule missing from written config:\n%s", data)
	}
}

func TestAppendProjectPermissionRulePreservesExistingKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".aegis")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "provider:\n  model: test-model\npermission:\n  mode: build\n  rules:\n    - deny write(/etc/*)\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AppendProjectPermissionRule(root, "allow shell(go test*)"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	s := string(data)
	for _, want := range []string{"test-model", "mode: build", "deny write(/etc/*)", "allow shell(go test*)"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q preserved/added, got:\n%s", want, s)
		}
	}
}

func TestAppendProjectPermissionRuleDeduplicates(t *testing.T) {
	root := t.TempDir()
	rule := "allow shell(npm test*)"
	if err := AppendProjectPermissionRule(root, rule); err != nil {
		t.Fatal(err)
	}
	if err := AppendProjectPermissionRule(root, rule); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".aegis", "config.yaml"))
	if got := strings.Count(string(data), rule); got != 1 {
		t.Fatalf("expected rule exactly once, found %d times:\n%s", got, data)
	}
}
