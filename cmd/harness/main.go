// Command harness is the entry point for the personal agent harness.
package main

import (
	"fmt"
	"os"

	"github.com/scottymacleod/agentharness/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
