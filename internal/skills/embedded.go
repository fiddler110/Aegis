package skills

import (
	"bytes"
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

// materializeTo extracts the embedded builtin skill tree to dest. When
// filter is non-nil, only top-level builtin directories whose lowercased
// name is in filter are written (fs.SkipDir on the rest) — used so a
// project only picks up the specific skill(s) it enabled/activated, not all
// embedded builtins. A file is only (re)written when its content differs
// from what's already on disk: commands like /threat-model call the
// activation path on every invocation, and a blind overwrite would bump
// every file's mtime each time, defeating skillsDirSignature's mtime-based
// Discover cache (P32.7) on every subsequent call in a session.
func materializeTo(dest string, filter map[string]bool) error {
	return fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("builtin", path)
		if err != nil {
			return err
		}
		if filter != nil && rel != "." {
			top := rel
			if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
				top = rel[:i]
			}
			if !filter[strings.ToLower(top)] {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, data) {
			return nil
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// MaterializeBuiltins extracts the embedded built-in skills to
// <dataDir>/builtin-skills/ so they can be read like any other bundled skill
// — including their companion assets, via the model's normal file tools. It
// keeps existing files in sync with the embedded content on every call so an
// upgraded binary's built-ins stay current; it never touches a project or
// user skills directory. A blank dataDir is a no-op.
func MaterializeBuiltins(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	return materializeTo(filepath.Join(dataDir, builtinSkillsDirName), nil)
}

// MaterializeBuiltinsToProject extracts only the named embedded builtin
// skills into <workDir>/.aegis/builtin-skills/, mirroring the per-user
// <dataDir>/builtin-skills/ layout but scoped inside the project workspace
// so the model's sandboxed file tools (confined to workDir) can reach a
// builtin skill's reference/skeleton assets — see discoverSpecs and
// withAssetManifest's dir= fallback, which resolves correctly once a
// skill's directory is a subpath of workDir. A blank workDir or empty names
// is a no-op.
func MaterializeBuiltinsToProject(workDir string, names []string) error {
	if workDir == "" || len(names) == 0 {
		return nil
	}
	return materializeTo(filepath.Join(workDir, ".aegis", builtinSkillsDirName), enabledSet(names))
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
