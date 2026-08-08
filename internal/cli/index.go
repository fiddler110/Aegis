package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/spf13/cobra"
)

// repoMapCachePath is the project-local cache file for the repository map.
func repoMapCachePath(root string) string {
	return filepath.Join(root, ".aegis", "repomap.json")
}

func newIndexCmd() *cobra.Command {
	var (
		maxBytes   int
		maxSymbols int
		print      bool
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// The cache this writes is what the daemon and `aegis chat` render
			// from, so its budget has to be the configured one by default —
			// otherwise indexing from the CLI would quietly resize the map every
			// session sees. An explicitly typed flag still wins, which is why the
			// override is keyed on Changed() rather than on a non-zero value:
			// --max-symbols-per-file=-1 ("uncapped") is a meaningful setting that
			// a zero-check would mistake for "not supplied".
			opts := repomap.Options{
				MaxBytes:          cfg.RepoMap.MaxBytes,
				MaxSymbolsPerFile: cfg.RepoMap.MaxSymbolsPerFile,
			}
			if cmd.Flags().Changed("max-bytes") {
				opts.MaxBytes = maxBytes
			}
			if cmd.Flags().Changed("max-symbols-per-file") {
				opts.MaxSymbolsPerFile = maxSymbols
			}
			m, err := repomap.Build(root, opts)
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
	// Both flag defaults are left at the zero value so cobra prints no "(default
	// N)" that would contradict the configured budget; the usage text names the
	// config key that actually decides when the flag is absent.
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 0, "cap the rendered map at this many bytes (default: repomap.max_bytes)")
	cmd.Flags().IntVar(&maxSymbols, "max-symbols-per-file", 0, "cap symbols rendered per file; negative means uncapped (default: repomap.max_symbols_per_file)")
	cmd.Flags().BoolVar(&print, "print", false, "print the rendered map to stdout")
	return cmd
}
