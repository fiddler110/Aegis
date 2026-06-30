package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scottymacleod/aegis/internal/repomap"
	"github.com/spf13/cobra"
)

// repoMapCachePath is the project-local cache file for the repository map.
func repoMapCachePath(root string) string {
	return filepath.Join(root, ".aegis", "repomap.json")
}

func newIndexCmd() *cobra.Command {
	var (
		maxBytes int
		print    bool
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build a compact repository map for the agent's system prompt",
		Long: "Walk the repository, extract top-level symbols (functions, types, classes) from " +
			"source files, and cache a compact map at .aegis/repomap.json. When present, the map is " +
			"injected into the model's system prompt and refreshed automatically when files change.",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			m, err := repomap.Build(root, repomap.Options{MaxBytes: maxBytes})
			if err != nil {
				return fmt.Errorf("build repo map: %w", err)
			}
			cache := repoMapCachePath(root)
			if err := m.Save(cache); err != nil {
				return fmt.Errorf("save repo map: %w", err)
			}
			out := cmd.OutOrStdout()
			if print {
				fmt.Fprintln(out, m.Render())
			}
			fmt.Fprintf(out, "Indexed %d files → %s\n", len(m.Files), cache)
			return nil
		},
	}
	cmd.Flags().IntVar(&maxBytes, "max-bytes", repomap.DefaultMaxBytes, "cap the rendered map at this many bytes")
	cmd.Flags().BoolVar(&print, "print", false, "print the rendered map to stdout")
	return cmd
}
