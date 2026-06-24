// Package commands loads user-defined slash commands from markdown files. Each
// file is a command template with YAML frontmatter (name, description, args)
// and a body that becomes the prompt. Commands are discovered from
// $DataDir/commands/*.md and .agentharness/commands/*.md.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Command is a user-defined slash command.
type Command struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Args        []string `json:"args"`        // named placeholders (e.g. ["file", "query"])
	Body        string   `json:"body"`        // the prompt template
	Source      string   `json:"source"`      // file path it was loaded from
}

// Expand replaces {{arg}} placeholders in the body with the given values.
func (c *Command) Expand(values map[string]string) string {
	body := c.Body
	for _, arg := range c.Args {
		placeholder := "{{" + arg + "}}"
		val := values[arg]
		body = strings.ReplaceAll(body, placeholder, val)
	}
	return body
}

// Registry holds discovered commands.
type Registry struct {
	commands map[string]*Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]*Command)}
}

// Register adds a command. Duplicates are silently overwritten (project-level
// commands override global ones).
func (r *Registry) Register(cmd *Command) {
	r.commands[cmd.Name] = cmd
}

// Get looks up a command by name.
func (r *Registry) Get(name string) (*Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns all commands sorted by name.
func (r *Registry) List() []*Command {
	out := make([]*Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Discover loads commands from the given directories. Later directories
// override earlier ones for same-named commands (project overrides global).
func Discover(dirs ...string) *Registry {
	reg := NewRegistry()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			cmd, err := parseCommandFile(path)
			if err != nil {
				continue
			}
			reg.Register(cmd)
		}
	}
	return reg
}

// CommandDirs returns the standard directories to search for commands.
func CommandDirs(dataDir, projectRoot string) []string {
	return []string{
		filepath.Join(dataDir, "commands"),
		filepath.Join(projectRoot, ".agentharness", "commands"),
	}
}

// parseCommandFile reads a markdown command file with YAML frontmatter.
// Format:
//
//	---
//	name: cmd-name
//	description: what it does
//	args: [file, query]
//	---
//	Body content with {{file}} and {{query}} placeholders.
func parseCommandFile(path string) (*Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	// Parse frontmatter.
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("missing frontmatter in %s", path)
	}
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("unclosed frontmatter in %s", path)
	}
	fm := strings.TrimSpace(parts[0])
	body := strings.TrimSpace(parts[1])

	cmd := &Command{Source: path, Body: body}

	// Simple YAML-ish parsing (no dependency).
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "name":
			cmd.Name = val
		case "description":
			cmd.Description = val
		case "args":
			cmd.Args = parseList(val)
		}
	}

	if cmd.Name == "" {
		// Fall back to filename.
		cmd.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return cmd, nil
}

// parseList parses a YAML-ish list: [a, b, c] or a, b, c.
func parseList(s string) []string {
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
