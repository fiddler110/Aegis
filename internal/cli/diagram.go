package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/scottymacleod/agentharness/internal/diagram"
	"github.com/spf13/cobra"
)

func newDiagramCmd() *cobra.Command {
	var (
		dtype  string
		format string
		out    string
	)

	cmd := &cobra.Command{
		Use:   "diagram [source-file]",
		Short: "Render a diagram from text (Mermaid, PlantUML, C4, Graphviz, …)",
		Long:  "Reads diagram source from a file argument or stdin and renders it via Kroki (with a local CLI fallback). Writes to --out, or prints SVG/draw.io to stdout.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			var source []byte
			if len(args) == 1 {
				source, err = os.ReadFile(args[0])
			} else {
				source, err = io.ReadAll(cmd.InOrStdin())
			}
			if err != nil {
				return err
			}

			r := diagram.DefaultChain(cfg.Diagram.KrokiURL)
			data, _, err := diagram.Render(cmd.Context(), r, dtype, string(source), format)
			if err != nil {
				return err
			}

			if out == "" {
				if format == diagram.FormatPNG || format == diagram.FormatPDF {
					return fmt.Errorf("binary format %q requires --out", format)
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d bytes)\n", out, len(data))
			return nil
		},
	}

	cmd.Flags().StringVar(&dtype, "type", "mermaid", "diagram notation")
	cmd.Flags().StringVar(&format, "format", "svg", "output format: svg, png, pdf, drawio")
	cmd.Flags().StringVar(&out, "out", "", "output file (default stdout for svg/drawio)")
	return cmd
}
