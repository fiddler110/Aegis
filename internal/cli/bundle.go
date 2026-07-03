package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		Use:   "info <path-or-git-url>",
		Short: "Show a bundle's manifest and the artifacts it would install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cleanup, err := resolveBundle(args[0])
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}
			b, err := bundle.Load(path)
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
			if hash, err := b.ContentHash(); err == nil {
				fmt.Fprintf(out, "\ncontent hash: %s\n(pin with `bundle install --expect-sha256 %s` to detect upstream changes)\n", hash, hash)
			}
			return nil
		},
	}
}

func newBundleInstallCmd() *cobra.Command {
	var scope string
	var overwrite bool
	var expectHash string
	cmd := &cobra.Command{
		Use:   "install <path-or-git-url>",
		Short: "Install a bundle into the project (default) or user scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cleanup, err := resolveBundle(args[0])
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}
			b, err := bundle.Load(path)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			// P7.6: a bundle installs arbitrary tool/persona code with no other
			// provenance check. --expect-sha256 lets a caller pin what they
			// reviewed (e.g. in a script or CI config) and abort before writing
			// anything if the upstream repo now serves something different.
			hash, hashErr := b.ContentHash()
			if expectHash != "" {
				if hashErr != nil {
					return fmt.Errorf("compute content hash: %w", hashErr)
				}
				want := expectHash
				if !strings.Contains(want, ":") {
					want = "sha256:" + want // allow the bare hex digest too
				}
				if !strings.EqualFold(hash, want) {
					return fmt.Errorf("bundle content hash mismatch: got %s, expected %s — refusing to install (the source may have changed)", hash, expectHash)
				}
			}

			dest, err := scopeDir(scope)
			if err != nil {
				return err
			}
			written, err := b.Install(dest, overwrite)
			if err != nil {
				return err
			}
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
			if hashErr == nil {
				fmt.Fprintf(out, "content hash: %s\n", hash)
				if expectHash == "" {
					fmt.Fprintf(out, "(pin with --expect-sha256 %s on future installs to detect upstream changes)\n", hash)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "project", "install scope: project (./.aegis) or user (data dir)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace artifacts that already exist")
	cmd.Flags().StringVar(&expectHash, "expect-sha256", "", "abort if the bundle's content hash doesn't match this pinned value (see `bundle info`)")
	return cmd
}

// resolveBundle returns a local directory path for the bundle, plus an optional
// cleanup func to remove a temporary clone. If src is a git URL it is cloned
// with --depth=1 into a temp dir; otherwise src is returned as-is.
func resolveBundle(src string) (path string, cleanup func(), err error) {
	if !isGitURL(src) {
		return src, nil, nil
	}
	dir, err := os.MkdirTemp("", "aegis-bundle-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	rm := func() { os.RemoveAll(dir) }
	out, cloneErr := exec.Command("git", "clone", "--depth=1", src, dir).CombinedOutput()
	if cloneErr != nil {
		rm()
		return "", nil, fmt.Errorf("git clone %s: %v\n%s", src, cloneErr, strings.TrimSpace(string(out)))
	}
	return dir, rm, nil
}

// isGitURL reports whether s looks like a remote git URL.
func isGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://") ||
		strings.HasPrefix(s, "ssh://")
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
