package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// builtinFS holds the skills Aegis ships embedded in the binary. Each is a
// bundled skill directory (builtin/<name>/SKILL.md, plus any companion
// assets) so the same loader/asset-manifest logic used for on-disk bundled
// skills applies unchanged once materialized to disk.
//
//go:embed builtin
var builtinFS embed.FS

// builtinSkillsDirName is the subdirectory of the per-user data directory
// built-in skills are extracted into.
const builtinSkillsDirName = "builtin-skills"

// MaterializeBuiltins extracts the embedded built-in skills to
// <dataDir>/builtin-skills/ so they can be read like any other bundled skill
// — including their companion assets, via the model's normal file tools. It
// overwrites existing files on every call so an upgraded binary's built-ins
// stay in sync; it never touches a project or user skills directory. A blank
// dataDir is a no-op.
func MaterializeBuiltins(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	dest := filepath.Join(dataDir, builtinSkillsDirName)
	return fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("builtin", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// BuiltinSkill describes one embedded built-in skill for listing purposes
// (CLI/TUI), independent of whether it is currently enabled.
type BuiltinSkill struct {
	Name        string
	Description string
}

// Builtins lists every embedded built-in skill, sorted by name, reading
// straight from the compiled-in filesystem so it works even before
// MaterializeBuiltins has run.
func Builtins() []BuiltinSkill {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil
	}
	var out []BuiltinSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + e.Name() + "/SKILL.md")
		if err != nil {
			continue
		}
		sk := parseSkill(e.Name(), string(data))
		out = append(out, BuiltinSkill{Name: sk.Name, Description: sk.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// IsBuiltin reports whether name matches an embedded built-in skill.
func IsBuiltin(name string) bool {
	for _, b := range Builtins() {
		if strings.EqualFold(b.Name, name) {
			return true
		}
	}
	return false
}
