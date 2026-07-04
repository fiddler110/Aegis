package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/spf13/cobra"
)

func newPersonaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: "List, inspect, and scaffold personas",
		Long: "Personas are named behavioral profiles (system prompt plus optional model, " +
			"permission mode, rules, and output-guard overrides). Built-ins ship with Aegis; " +
			"custom personas are *.md files with YAML frontmatter in the user or project " +
			"personas directory. The daemon reloads persona files automatically — edits " +
			"take effect on the next message, no restart needed.",
	}
	cmd.AddCommand(newPersonaListCmd())
	cmd.AddCommand(newPersonaShowCmd())
	cmd.AddCommand(newPersonaNewCmd())
	cmd.AddCommand(newPersonaUseCmd())
	return cmd
}

// loadPersonaFiles refreshes the file-loaded persona set from the standard
// directories so list/show reflect what the daemon would use, and returns the
// resolved config so callers can read DefaultPersona.
func loadPersonaFiles() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	dirs := persona.DiscoverDirs(cfg.DataDir, cwd)
	persona.Refresh(dirs...)
	return cfg, nil
}

func newPersonaListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available personas (built-in and custom)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadPersonaFiles()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			w := 0
			for _, name := range persona.Names() {
				if len(name) > w {
					w = len(name)
				}
			}
			for _, name := range persona.Names() {
				p, _ := persona.Get(name)
				var markers []string
				if p.Loaded {
					markers = append(markers, "custom")
				}
				if cfg.DefaultPersona == p.Name {
					markers = append(markers, "default")
				}
				suffix := ""
				if len(markers) > 0 {
					suffix = "  [" + strings.Join(markers, ", ") + "]"
				}
				fmt.Fprintf(out, "%-*s  %s%s\n", w, p.Name, p.Description, suffix)
			}
			return nil
		},
	}
}

func newPersonaShowCmd() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a persona's profile: source, model, mode, rules, guard, and prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadPersonaFiles()
			if err != nil {
				return err
			}
			p, ok := persona.Get(args[0])
			if !ok {
				return fmt.Errorf("unknown persona %q (run: aegis persona list)", args[0])
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Name:        %s\n", p.Name)
			fmt.Fprintf(out, "Description: %s\n", p.Description)
			if p.Loaded {
				fmt.Fprintf(out, "Source:      %s (edit this file; changes apply without restart)\n", p.Path)
			} else {
				fmt.Fprintln(out, "Source:      built-in")
			}
			if cfg.DefaultPersona == p.Name {
				fmt.Fprintln(out, "Default:     yes — new sessions in this project start with this persona")
			}
			if p.Model != "" {
				fmt.Fprintf(out, "Model:       %s\n", p.Model)
			}
			if p.Mode != "" {
				fmt.Fprintf(out, "Mode:        %s\n", p.Mode)
			}
			if len(p.Tools) > 0 {
				fmt.Fprintf(out, "Tools:       %s (advisory — off-list tools warn/ask, never blocked)\n", strings.Join(p.Tools, ", "))
			}
			for _, r := range p.Rules {
				fmt.Fprintf(out, "Rule:        %s\n", r)
			}
			if g := p.Guard; g != nil {
				switch {
				case g.Disabled:
					fmt.Fprintln(out, "Guard:       disabled")
				case g.Mode == "schema":
					fmt.Fprintf(out, "Guard:       schema (keys: %s)\n", strings.Join(g.Schema, ", "))
				default:
					fmt.Fprintf(out, "Guard:       %s rubric: %s\n", g.Mode, g.Rubric)
				}
			}
			prompt := p.System
			const previewLen = 600
			if !full && len(prompt) > previewLen {
				prompt = prompt[:previewLen] + "\n… (" + fmt.Sprint(len(p.System)-previewLen) + " more chars; use --full)"
			}
			fmt.Fprintf(out, "\n%s\n", prompt)
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "print the entire system prompt")
	return cmd
}

const personaTemplate = `---
description: %s
# Everything below is optional — delete what you don't need.
# model: claude-opus-4-8          # pin a model for this persona
# mode: plan                      # permission mode: plan | build | auto
# tools: [read_file, grep, shell] # advisory: off-list tool use warns/asks, never blocked
# rules:                          # permission rules merged into the session
#   - "deny write(*)"
#   - "allow shell(git diff*)"
# output_guard:                   # per-persona output validation
#   mode: llm
#   rubric: "Every finding cites file:line."
#   max_retries: 2
---
You are Aegis operating as a %s.

Describe the role's focus, responsibilities, and working style here.
This markdown body becomes the persona's system prompt.

## Completing your output
State what a complete answer for this role must include.
`

func newPersonaNewCmd() *cobra.Command {
	var global bool
	var description string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a custom persona file (project .aegis/personas by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			if name == "" || strings.ContainsAny(name, `/\`) {
				return fmt.Errorf("persona name must be a simple name like %q", "incident-responder")
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			dir := filepath.Join(".aegis", "personas")
			if global {
				dir = filepath.Join(cfg.DataDir, "personas")
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			path := filepath.Join(dir, name+".md")
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists — edit it directly (changes apply without restart)", path)
			}
			if description == "" {
				description = strings.ReplaceAll(name, "-", " ") + " persona"
			}
			roleTitle := strings.ToUpper(strings.ReplaceAll(name, "-", " "))
			content := fmt.Sprintf(personaTemplate, description, roleTitle)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created %s\n", path)
			fmt.Fprintf(out, "Edit the prompt, then switch to it with /persona %s — no restart needed.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "create in the user-global personas directory instead of the project")
	cmd.Flags().StringVar(&description, "description", "", "one-line description shown in persona listings")
	return cmd
}

func newPersonaUseCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default persona new sessions start with",
		Long: "Sets default_persona so `aegis` (and `aegis --resume` for new sessions) starts " +
			"with this persona without needing --persona every time. Writes to the project " +
			"config (.aegis/config.yaml) by default so it travels with the repo; --global " +
			"writes to the user config instead. An explicit --persona flag always overrides this.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			if _, err := loadPersonaFiles(); err != nil {
				return err
			}
			if _, ok := persona.Get(name); !ok {
				return fmt.Errorf("unknown persona %q (run: aegis persona list)", name)
			}
			write := config.PatchProjectDefaultPersona
			scope := "project (.aegis/config.yaml)"
			if global {
				write = config.PatchGlobalDefaultPersona
				scope = "global"
			}
			if err := write(name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "default persona set to %q (%s config). New sessions will start with it unless --persona overrides.\n", name, scope)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write to the user-global config instead of the project")
	return cmd
}
