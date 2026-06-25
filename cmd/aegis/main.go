// Command aegis is the entry point for the Aegis AI agent harness.
package main

import (
	"fmt"
	"os"

	"github.com/scottymacleod/aegis/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
