package config

import (
	"fmt"
	"os"
	"path/filepath"

	yamlv3 "go.yaml.in/yaml/v3"
)

// AppendProjectPermissionRule appends a text permission rule (e.g.
// "allow shell(npm test*)") to the project-level config file
// (<root>/.aegis/config.yaml → permission.rules), creating the file and
// directory as needed. Appending an already-present rule is a no-op. Other
// keys in the file are preserved.
//
// This backs the TUI's "allow always for this pattern" approval option (TQ6):
// the rule takes effect in the running daemon immediately and this write makes
// it survive restarts.
func AppendProjectPermissionRule(root, rule string) error {
	path := filepath.Join(root, ".aegis", "config.yaml")

	doc := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yamlv3.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if doc == nil {
			doc = map[string]any{}
		}
	case os.IsNotExist(err):
		// fresh file
	default:
		return fmt.Errorf("read %s: %w", path, err)
	}

	perm, _ := doc["permission"].(map[string]any)
	if perm == nil {
		perm = map[string]any{}
		doc["permission"] = perm
	}
	rules, _ := perm["rules"].([]any)
	for _, r := range rules {
		if s, ok := r.(string); ok && s == rule {
			return nil // already present
		}
	}
	perm["rules"] = append(rules, rule)

	out, err := yamlv3.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, out, 0o644)
}
