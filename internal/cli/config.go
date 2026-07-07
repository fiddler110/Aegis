package cli

import (
	"encoding/json"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the resolved configuration (API keys redacted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.Provider.APIKey != "" {
				cfg.Provider.APIKey = "***redacted***"
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(cfg)
		},
	}
	cmd.AddCommand(newConfigUpdateCmd())
	return cmd
}
