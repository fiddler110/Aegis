// Command webuimanifest prints the current embedded web UI bundle's digest,
// for regenerating internal/server/webui/dist.sha256 after `npm run build`
// (P81.17). Run: go run ./cmd/webuimanifest > internal/server/webui/dist.sha256
package main

import (
	"fmt"
	"os"

	"github.com/fiddler110/aegis/internal/server"
)

func main() {
	digest, err := server.ComputeDistDigest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "webuimanifest:", err)
		os.Exit(1)
	}
	fmt.Println(digest)
}
