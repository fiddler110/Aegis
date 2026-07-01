package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/scottymacleod/aegis/internal/tool"
)

// rgPath is set once at startup; empty string means fall back to WalkDir.
var rgPath, _ = exec.LookPath("rg")

// --- glob ---

type globTool struct{ root string }

func (t *globTool) Name() string                { return "glob" }
func (t *globTool) Capability() tool.Capability { return tool.CapRead }
func (t *globTool) Description() string {
	return "Find files in the workspace matching a glob pattern (e.g. **/*.go). Returns matching paths."
}
func (t *globTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"pattern":{"type":"string","description":"glob pattern, ** matches any depth"}},"required":["pattern"]}`)
}
func (t *globTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return tool.Result{Content: "pattern is required", IsError: true}, nil
	}

	if rgPath != "" {
		return t.executeRg(ctx, args.Pattern)
	}

	var matches []string
	walkErr := filepath.WalkDir(t.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != t.root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(t.root, path)
		rel = filepath.ToSlash(rel)
		if matchGlob(args.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if walkErr != nil {
		return tool.Result{Content: fmt.Sprintf("walk failed: %v", walkErr), IsError: true}, nil
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return tool.Result{Content: "no files matched"}, nil
	}
	if len(matches) > 1000 {
		matches = matches[:1000]
	}
	return tool.Result{Content: strings.Join(matches, "\n")}, nil
}

func (t *globTool) executeRg(ctx context.Context, pattern string) (tool.Result, error) {
	cmd := exec.CommandContext(ctx, rgPath, "--files", "--hidden", "-g", pattern, "--", t.root)
	out, _ := cmd.Output() // exit 1 = no matches, not an error
	if len(bytes.TrimSpace(out)) == 0 {
		return tool.Result{Content: "no files matched"}, nil
	}
	var matches []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		rel, err := filepath.Rel(t.root, line)
		if err != nil {
			rel = line
		}
		matches = append(matches, filepath.ToSlash(rel))
	}
	sort.Strings(matches)
	if len(matches) > 1000 {
		matches = matches[:1000]
	}
	if len(matches) == 0 {
		return tool.Result{Content: "no files matched"}, nil
	}
	return tool.Result{Content: strings.Join(matches, "\n")}, nil
}

// matchGlob supports ** (any depth) in addition to standard path.Match syntax.
func matchGlob(pattern, name string) bool {
	if strings.Contains(pattern, "**") {
		re := globToRegexp(pattern)
		return re.MatchString(name)
	}
	ok, err := filepath.Match(pattern, name)
	if err == nil && ok {
		return true
	}
	// Also try matching just the base name for convenience (e.g. "*.go").
	base, _ := filepath.Match(pattern, filepath.Base(name))
	return base
}

func globToRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile("$^") // matches nothing
	}
	return re
}

// --- grep ---

type grepTool struct{ root string }

func (t *grepTool) Name() string                { return "grep" }
func (t *grepTool) Capability() tool.Capability { return tool.CapRead }
func (t *grepTool) Description() string {
	return "Search workspace file contents with a regular expression. Returns matching lines as path:line:text."
}
func (t *grepTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"pattern":{"type":"string","description":"RE2 regular expression"},"glob":{"type":"string","description":"optional file glob filter"},"ignore_case":{"type":"boolean"}},"required":["pattern"]}`)
}
func (t *grepTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}

	if rgPath != "" {
		return t.executeRg(ctx, args.Pattern, args.Glob, args.IgnoreCase)
	}

	pat := args.Pattern
	if args.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("invalid regexp: %v", err), IsError: true}, nil
	}

	var out []string
	const maxMatches = 500
	walkErr := filepath.WalkDir(t.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != t.root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(t.root, path)
		rel = filepath.ToSlash(rel)
		if args.Glob != "" && !matchGlob(args.Glob, rel) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d:%s", rel, i+1, strings.TrimRight(line, "\r")))
				if len(out) >= maxMatches {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != context.Canceled {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", walkErr), IsError: true}, nil
	}
	if len(out) == 0 {
		return tool.Result{Content: "no matches"}, nil
	}
	return tool.Result{Content: strings.Join(out, "\n")}, nil
}

func (t *grepTool) executeRg(ctx context.Context, pattern, glob string, ignoreCase bool) (tool.Result, error) {
	argv := []string{"--line-number", "--no-heading", "--color=never", "--hidden", "-e", pattern}
	if ignoreCase {
		argv = append(argv, "-i")
	}
	if glob != "" {
		argv = append(argv, "-g", glob)
	}
	argv = append(argv, "--", t.root)

	cmd := exec.CommandContext(ctx, rgPath, argv...)
	raw, _ := cmd.Output() // exit 1 = no matches
	if len(bytes.TrimSpace(raw)) == 0 {
		return tool.Result{Content: "no matches"}, nil
	}

	var results []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		// rg outputs: /abs/path/file:lineno:content — strip root prefix
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			rel, err := filepath.Rel(t.root, parts[0])
			if err != nil {
				rel = parts[0]
			}
			line = filepath.ToSlash(rel) + ":" + parts[1] + ":" + parts[2]
		}
		results = append(results, line)
		if len(results) >= 500 {
			break
		}
	}
	if len(results) == 0 {
		return tool.Result{Content: "no matches"}, nil
	}
	return tool.Result{Content: strings.Join(results, "\n")}, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".idea", ".vscode", "dist", "build", ".aegis":
		return true
	}
	return false
}

func isBinary(data []byte) bool {
	n := min(len(data), 8000)
	for i := range n {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
