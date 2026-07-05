package cli

import (
	"fmt"
	"os"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/embed"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/spf13/cobra"
)

func newKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage the project knowledge base used by the project_knowledge tool",
	}
	cmd.AddCommand(newKnowledgeIndexCmd())
	return cmd
}

func newKnowledgeIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Rebuild the project knowledge base FTS5 index",
		Long: "Walks the project, indexing documentation files (*.md, *.txt, *.rst) and Go doc " +
			"comments into a searchable index used by the project_knowledge tool. When " +
			"embeddings.enabled is set in config, also computes semantic embeddings for hybrid " +
			"recall alongside the default keyword search.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			embedder := embed.New(cfg.Embeddings.Enabled, cfg.Embeddings.Provider, cfg.Embeddings.Model, cfg.Embeddings.BaseURL)
			dbPath := cfg.KnowledgeDBPath(root)
			store, err := knowledge.Open(root, dbPath, embedder)
			if err != nil {
				return fmt.Errorf("open knowledge store: %w", err)
			}
			defer store.Close()

			n, err := store.Index(cmd.Context())
			if err != nil {
				return fmt.Errorf("index: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Indexed %d documents → %s\n", n, dbPath)
			if cfg.Embeddings.Enabled {
				fmt.Fprintf(out, "Semantic embeddings: enabled (%s / %s)\n", cfg.Embeddings.Provider, cfg.Embeddings.Model)
			}
			return nil
		},
	}
	return cmd
}
