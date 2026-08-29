package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
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
			// A missing TLS cert here just means no daemon has ever started at
			// this data dir yet — treat it the same as "no daemon reachable".
			// cleanup scrubs the token of whichever client ends up used
			// (FIND-33/P24.21) and stops the daemon if this process started it.
			cl, embedded, cleanup, err := connectOrStartDaemon(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			url := webUIURL(cfg.Server.Addr, cfg.Server.TLS.Enabled)
			if cfg.Server.TLS.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "TLS is enabled: your browser will warn about the daemon's self-signed certificate — this is expected (see docs/configuration.md).")
			}

			warnSandboxFallback(cl)
			if !embedded {
				// A daemon was already running; just point the browser at it
				// and leave it running.
				fmt.Fprintf(cmd.OutOrStdout(), "Web UI: %s\n", url)
				if !noOpen {
					_ = openBrowser(url)
				}
				return nil
			}

			// This process owns the daemon, so it has to stay alive for the UI
			// to keep working.
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

func webUIURL(addr string, tlsEnabled bool) string {
	// addr is a host:port like 127.0.0.1:4127; ensure it has a host.
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	scheme := "http://"
	if tlsEnabled {
		scheme = "https://"
	}
	return scheme + addr + "/ui"
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
