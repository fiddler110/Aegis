package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scottymacleod/aegis/internal/bundle"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/spf13/cobra"
)

func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Install bundles of commands, agents, and skills",
		Long: "A bundle is a directory with a bundle.yaml manifest plus commands/, agents/, and " +
			"skills/ subdirectories. Installing copies those artifacts into a scope so they're picked " +
			"up like any hand-authored command, agent, or skill.",
	}
	cmd.AddCommand(newBundleInfoCmd(), newBundleInstallCmd())
	return cmd
}

func newBundleInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <path>",
		Short: "Show a bundle's manifest and the artifacts it would install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bundle.Load(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			m := b.Manifest
			fmt.Fprintf(out, "%s %s\n", m.Name, m.Version)
			if m.Description != "" {
				fmt.Fprintf(out, "%s\n", m.Description)
			}
			if m.Author != "" {
				fmt.Fprintf(out, "by %s\n", m.Author)
			}
			fmt.Fprintf(out, "\n%d artifact(s):\n", len(b.Artifacts))
			for _, a := range b.Artifacts {
				fmt.Fprintf(out, "  %s/%s\n", a.Kind, a.Rel)
			}
			return nil
		},
	}
}

func newBundleInstallCmd() *cobra.Command {
	var scope string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "install <path>",
		Short: "Install a bundle into the project (default) or user scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := bundle.Load(args[0])
			if err != nil {
				return err
			}
			dest, err := scopeDir(scope)
			if err != nil {
				return err
			}
			written, err := b.Install(dest, overwrite)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(written) == 0 {
				fmt.Fprintf(out, "nothing installed (all artifacts already exist; use --overwrite to replace)\n")
				return nil
			}
			fmt.Fprintf(out, "installed %q (%d file(s)) into %s:\n", b.Manifest.Name, len(written), dest)
			for _, w := range written {
				fmt.Fprintf(out, "  %s\n", w)
			}
			skipped := len(b.Artifacts) - len(written)
			if skipped > 0 {
				fmt.Fprintf(out, "(%d already present, skipped; --overwrite to replace)\n", skipped)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "project", "install scope: project (./.aegis) or user (data dir)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace artifacts that already exist")
	return cmd
}

// scopeDir resolves the destination root for the chosen scope.
func scopeDir(scope string) (string, error) {
	switch scope {
	case "", "project":
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".aegis"), nil
	case "user":
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.DataDir, nil
	default:
		return "", fmt.Errorf("unknown scope %q (use project or user)", scope)
	}
}
