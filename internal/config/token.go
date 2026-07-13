package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// GenerateAndWriteToken creates a cryptographically random hex token and
// writes it to path with owner-only permissions, mirroring the daemon's own
// bearer-token bootstrap (internal/server's generateAndWriteToken for
// daemon.token). It is shared by every stdio-auth interface that needs the
// same "generate on start, write to a data-dir file the reading side can
// discover" pattern — currently `aegis mcp-serve` (MCPTokenPath) and
// `aegis acp` (ACPTokenPath), added for P27.4/FIND-06.
//
// The 0o600 mode bit is sufficient on POSIX but cosmetic on Windows, where a
// new file inherits its parent directory's ACL rather than deriving
// permissions from the mode argument — on a shared Windows host another
// local account can often still read the file. fsguard.RestrictToOwner
// applies a real, non-inherited ACL restricting the file to its owner on
// Windows and is a no-op on POSIX (see internal/fsguard).
func GenerateAndWriteToken(path string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	if err := fsguard.RestrictToOwner(path); err != nil {
		return "", fmt.Errorf("restrict token file permissions: %w", err)
	}
	return token, nil
}
