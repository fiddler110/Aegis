package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List and toggle the skills built into Aegis",
		Long: "Aegis ships several skills (progressive-disclosure playbooks for tasks like " +
			"code review, security audits, diagramming, and debugging) embedded in the binary. " +
			"They stay dormant — costing zero system-prompt tokens — until enabled by name. " +
			"Project (.aegis/skills) and user (~/.aegis/skills) skill files are separate and " +
			"are always active regardless of this list.",
	}
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsEnableCmd())
	cmd.AddCommand(newSkillsDisableCmd())
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List built-in skills and whether each is enabled",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			enabled := enabledBuiltinSet(cfg)

			out := cmd.OutOrStdout()
			builtins := skills.Builtins()
			if len(builtins) == 0 {
				fmt.Fprintln(out, "No built-in skills found.")
				return nil
			}
			w := 0
			for _, b := range builtins {
				if len(b.Name) > w {
					w = len(b.Name)
				}
			}
			for _, b := range builtins {
				status := "disabled"
				if enabled[strings.ToLower(b.Name)] {
					status = "enabled"
				}
				fmt.Fprintf(out, "%-*s  [%-8s]  %s\n", w, b.Name, status, b.Description)
			}
			fmt.Fprintln(out, "\nEnable with: aegis skills enable <name> [--global]")
			return nil
		},
	}
}

func newSkillsEnableCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "Turn on a built-in skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return toggleBuiltinSkill(cmd, args[0], true, global)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write to the user-global config instead of the project")
	return cmd
}

func newSkillsDisableCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "Turn off a built-in skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return toggleBuiltinSkill(cmd, args[0], false, global)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write to the user-global config instead of the project")
	return cmd
}

func enabledBuiltinSet(cfg *config.Config) map[string]bool {
	set := make(map[string]bool, len(cfg.Skills.BuiltinEnabled))
	for _, n := range cfg.Skills.BuiltinEnabled {
		set[strings.ToLower(strings.TrimSpace(n))] = true
	}
	return set
}

// toggleBuiltinSkill enables or disables a built-in skill by writing the full
// desired enabled set back to config — koanf's layered config replaces a
// list-valued key entirely at whichever layer sets it rather than merging
// element-wise, so the effective (already-merged) list must be the write's
// starting point, not just this layer's own prior list.
func toggleBuiltinSkill(cmd *cobra.Command, name string, enable, global bool) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if !skills.IsBuiltin(name) {
		return fmt.Errorf("unknown built-in skill %q (run: aegis skills list)", name)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	set := enabledBuiltinSet(cfg)
	if enable {
		set[name] = true
	} else {
		delete(set, name)
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)

	write := config.PatchProjectSkillsEnabled
	scope := "project (.aegis/config.yaml)"
	if global {
		write = config.PatchGlobalSkillsEnabled
		scope = "global"
	}
	if err := write(names); err != nil {
		return err
	}
	verb := "enabled"
	if !enable {
		verb = "disabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %q (%s config). Restart Aegis (or the daemon) to apply.\n", verb, name, scope)
	return nil
}
