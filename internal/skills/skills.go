// Package skills discovers and loads skill definition files from well-known
// locations. Skills use progressive disclosure (P4.3): at session start only a
// compact index of skill names and descriptions is injected into the system
// prompt; the full body of a skill is loaded on demand via the `skill` tool.
//
// A skill file may carry YAML frontmatter with a `description:` (and optional
// `name:`) field. Skills that declare a description participate in progressive
// disclosure. Skills without a description fall back to eager injection so
// legacy skill files keep working unchanged.
//
// A skill can be either a single flat file or a directory bundling companion
// assets (templates, scripts, reference docs) alongside the instructions:
//
//	.aegis/skills/deploy.md              (flat)
//	.aegis/skills/html-report/SKILL.md   (bundled, name = directory name)
//	.aegis/skills/html-report/template.html
//	.aegis/skills/html-report/validate.py
//
// For a bundled skill, Dir holds the directory's path (relative to workDir
// when possible) and Content carries a trailing <skill_assets> manifest
// listing sibling files so the model knows to read them with its normal file
// tools — the skill loader does not read asset contents itself.
//
// Search order (first match wins per name):
//  1. .aegis/skills/  in the current working directory (project-local)
//  2. ~/.aegis/skills/  in the user's home directory (global)
//  3. embedded built-in skills (see embedded.go), filtered to whichever names
//     are passed as enabledBuiltins — these ship in the binary but stay
//     dormant (no system-prompt cost) unless named explicitly, since unlike
//     project/user skill files a user didn't choose to author them.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/trust"
)

// Skill represents a loaded skill definition.
type Skill struct {
	Name        string // from frontmatter `name:`, else the filename/directory stem
	Description string // from frontmatter `description:`; empty means eager-inject
	Content     string // markdown body with frontmatter stripped (plus asset manifest, if bundled)
	Dir         string // non-empty for a bundled (directory) skill; path to its companion files
}

// Discover loads all skills from the project and user directories, plus any
// embedded built-in skill named in enabledBuiltins (dataDir locates the
// per-user data directory built-ins are materialized into; see
// MaterializeBuiltins). Any error reading a directory is silently skipped so
// a missing .aegis/ folder doesn't break startup.
func Discover(workDir, dataDir string, enabledBuiltins []string) []Skill {
	seen := make(map[string]bool) // entry name → already loaded
	var skills []Skill

	// Project-local skills take precedence. Neither project nor user skill
	// files are built into the binary, so their bodies are treated as
	// untrusted (see appendFromDir's trusted param / FIND-05).
	projectDir := filepath.Join(workDir, ".aegis", "skills")
	skills = appendFromDir(skills, workDir, projectDir, seen, nil, false)

	// User-global skills fill in anything not overridden by the project.
	if home, err := os.UserHomeDir(); err == nil {
		userDir := filepath.Join(home, ".aegis", "skills")
		skills = appendFromDir(skills, workDir, userDir, seen, nil, false)
	}

	// Embedded built-ins fill in last, and only the ones explicitly enabled —
	// they never override a same-named project/user skill. These ship in the
	// binary, so they're trusted and not wrapped as untrusted content.
	if enabled := enabledSet(enabledBuiltins); len(enabled) > 0 && dataDir != "" {
		builtinDir := filepath.Join(dataDir, builtinSkillsDirName)
		skills = appendFromDir(skills, workDir, builtinDir, seen, enabled, true)
	}

	return skills
}

// enabledSet lowercases and dedupes a list of enabled builtin skill names for
// filter lookups.
func enabledSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" {
			set[n] = true
		}
	}
	return set
}

// appendFromDir loads every skill entry in dir. When filter is non-nil, only
// entries whose stem (filename without extension, or directory name) is
// present in filter are loaded — used to gate embedded built-ins behind an
// enabled-list without a second directory-walking implementation. trusted is
// true only for the embedded-builtins directory; every other source (project/
// user skill files) has its body wrapped in an untrusted-provenance marker
// before it can reach the system prompt (FIND-05/P24.4), since those files
// are arbitrary content from disk, not compiled into the binary.
func appendFromDir(dst []Skill, workDir, dir string, seen map[string]bool, filter map[string]bool, trusted bool) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst
	}
	for _, e := range entries {
		if seen[e.Name()] {
			continue
		}
		if filter != nil {
			stem := e.Name()
			if !e.IsDir() {
				stem = strings.TrimSuffix(stem, filepath.Ext(stem))
			}
			if !filter[strings.ToLower(stem)] {
				continue
			}
		}
		if e.IsDir() {
			skillDir := filepath.Join(dir, e.Name())
			manifestName := findSkillFile(skillDir)
			if manifestName == "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(skillDir, manifestName))
			if err != nil {
				continue
			}
			seen[e.Name()] = true
			sk := parseSkill(e.Name(), string(data))
			if !trusted {
				sk.Content = wrapUntrustedSkill(sk.Name, sk.Content)
			}
			sk.Dir = skillDir
			sk.Content = withAssetManifest(sk.Content, workDir, skillDir, manifestName)
			dst = append(dst, sk)
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		seen[e.Name()] = true
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sk := parseSkill(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), string(data))
		if !trusted {
			sk.Content = wrapUntrustedSkill(sk.Name, sk.Content)
		}
		dst = append(dst, sk)
	}
	return dst
}

// wrapUntrustedSkill tags a project/user skill file's body with the same
// untrusted-provenance marker used for MCP/web tool output and file-loaded
// personas, framing it as data rather than instructions for the model.
// scan=false for the same reason as the persona wrap (internal/persona/load.go):
// this content is re-injected every session, and skill prose routinely
// discusses its own instructions, so the heuristic scan would be noisy here;
// the provenance framing itself is the mitigation FIND-05 asks for.
func wrapUntrustedSkill(name, content string) string {
	if content == "" {
		return content
	}
	return trust.Wrap("skill_untrusted_content", [][2]string{{"skill", name}},
		"a skill file loaded from this project's or the user's .aegis/skills/ directory, not a built-in",
		content, false)
}

// findSkillFile returns the manifest filename ("SKILL.md", case-insensitive)
// inside a candidate skill directory, or "" if the directory isn't a skill
// bundle.
func findSkillFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "SKILL.md") {
			return e.Name()
		}
	}
	return ""
}

// withAssetManifest appends a <skill_assets> block listing every file under
// skillDir (recursively, e.g. references/foo.md or scripts/bar.py) other
// than the manifest itself, so the model knows what companion
// templates/scripts/references it can load with its own file tools.
func withAssetManifest(content, workDir, skillDir, manifestName string) string {
	var assets []string
	_ = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		if filepath.Dir(rel) == "." && strings.EqualFold(rel, manifestName) {
			return nil
		}
		assets = append(assets, filepath.ToSlash(rel))
		return nil
	})
	if len(assets) == 0 {
		return content
	}
	sort.Strings(assets)

	display := skillDir
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, skillDir); err == nil && !strings.HasPrefix(rel, "..") {
			display = rel
		}
	}

	var sb strings.Builder
	sb.WriteString(content)
	sb.WriteString("\n\n<skill_assets dir=\"")
	sb.WriteString(filepath.ToSlash(display))
	sb.WriteString("\">\n")
	sb.WriteString("Read these with your file tools before proceeding; do not fabricate their contents.\n")
	for _, a := range assets {
		sb.WriteString("- ")
		sb.WriteString(a)
		sb.WriteString("\n")
	}
	sb.WriteString("</skill_assets>")
	return sb.String()
}

// parseSkill extracts optional YAML frontmatter (name/description) and returns a
// Skill with the frontmatter stripped from the body. defaultName is used when
// the frontmatter carries no explicit name.
func parseSkill(defaultName, raw string) Skill {
	sk := Skill{Name: defaultName, Content: strings.TrimSpace(raw)}
	body, front, ok := splitFrontmatter(raw)
	if !ok {
		return sk
	}
	sk.Content = strings.TrimSpace(body)
	for _, line := range strings.Split(front, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), `"'`))
		switch key {
		case "name":
			if val != "" {
				sk.Name = val
			}
		case "description":
			sk.Description = val
		}
	}
	return sk
}

// splitFrontmatter separates a leading `---`-delimited YAML block from the body.
// Returns (body, frontmatter, true) when frontmatter is present.
func splitFrontmatter(raw string) (body, front string, ok bool) {
	trimmed := strings.TrimLeft(strings.TrimPrefix(raw, "\ufeff"), " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return raw, "", false
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return raw, "", false
	}
	front = rest[:end]
	body = rest[end+len("\n---"):]
	// Drop the remainder of the closing delimiter line.
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	return body, front, true
}

// Load returns a single discovered skill by name (matching either the
// frontmatter name or the filename), loading its full body on demand.
func Load(workDir, dataDir string, enabledBuiltins []string, name string) (Skill, bool) {
	for _, sk := range Discover(workDir, dataDir, enabledBuiltins) {
		if strings.EqualFold(sk.Name, name) {
			return sk, true
		}
	}
	return Skill{}, false
}

// BuildBlock returns a <skills>…</skills> XML block with the full body of every
// discovered project/user skill (no built-ins). Retained for callers that
// want eager injection of all skills regardless of frontmatter.
func BuildBlock(workDir string) string {
	loaded := Discover(workDir, "", nil)
	if len(loaded) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<skills>\n")
	for _, sk := range loaded {
		fmt.Fprintf(&sb, "<skill name=%q>\n%s\n</skill>\n", sk.Name, sk.Content)
	}
	sb.WriteString("</skills>")
	return sb.String()
}

// BuildIndex implements progressive disclosure. Skills that declare a
// description are listed compactly (name — description) under a
// <skills_available> block, telling the model to load the full body with the
// `skill` tool. Skills without a description are eager-injected in full for
// backward compatibility. Returns an empty string when no skills exist.
func BuildIndex(workDir, dataDir string, enabledBuiltins []string) string {
	loaded := Discover(workDir, dataDir, enabledBuiltins)
	if len(loaded) == 0 {
		return ""
	}
	var described, legacy []Skill
	for _, sk := range loaded {
		if sk.Description != "" {
			described = append(described, sk)
		} else {
			legacy = append(legacy, sk)
		}
	}

	var sb strings.Builder
	if len(described) > 0 {
		sb.WriteString("<skills_available>\n")
		sb.WriteString("These skills can be loaded on demand. When a task matches one, call the `skill` tool with its name to load the full instructions before proceeding.\n")
		for _, sk := range described {
			fmt.Fprintf(&sb, "- %s: %s\n", sk.Name, sk.Description)
		}
		sb.WriteString("</skills_available>")
	}
	for _, sk := range legacy {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "<skill name=%q>\n%s\n</skill>", sk.Name, sk.Content)
	}
	return sb.String()
}

// InjectIntoSystem appends the progressive-disclosure skills index to a system
// prompt. Returns base unchanged when no skills are found.
func InjectIntoSystem(base, workDir, dataDir string, enabledBuiltins []string) string {
	block := BuildIndex(workDir, dataDir, enabledBuiltins)
	if block == "" {
		return base
	}
	if base == "" {
		return block
	}
	return base + "\n\n" + block
}
