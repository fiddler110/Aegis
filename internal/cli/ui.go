package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/spf13/cobra"
)

func newUICmd() *cobra.Command {
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the Aegis web UI in a browser (over the local daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl := client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())

			healthCtx, healthCancel := context.WithTimeout(cmd.Context(), 2*time.Second)
			healthErr := cl.Health(healthCtx)
			healthCancel()

			url := webUIURL(cfg.Server.Addr)

			if healthErr == nil {
				// A daemon is already running; just point the browser at it.
				fmt.Fprintf(cmd.OutOrStdout(), "Web UI: %s\n", url)
				if !noOpen {
					_ = openBrowser(url)
				}
				return nil
			}

			// No daemon: start one embedded and keep it alive while the UI is used.
			stopDaemon, startErr := startEmbeddedDaemon(cfg)
			if startErr != nil {
				return fmt.Errorf("start daemon: %w", startErr)
			}
			defer stopDaemon()
			if !waitForDaemon(cl, 10*time.Second) {
				return fmt.Errorf("daemon at %s did not become ready within 10s", cfg.Server.Addr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Web UI: %s  (Ctrl+C to stop the daemon)\n", url)
			if !noOpen {
				_ = openBrowser(url)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			<-ctx.Done()
			return nil
		},
	}

	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL but do not launch a browser")
	return cmd
}

func webUIURL(addr string) string {
	// addr is a host:port like 127.0.0.1:4127; ensure it has a host.
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr + "/ui"
}

// openBrowser opens url in the default browser, best effort.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
