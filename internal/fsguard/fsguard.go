// Package fsguard hardens filesystem permissions on locally-stored,
// security-sensitive Aegis files (the daemon auth token, the SQLite session
// database and its WAL sidecars, .aegis/.env) beyond what a POSIX mode bit
// alone can express on Windows.
//
// On POSIX, os.WriteFile's mode argument (e.g. 0o600) already restricts a
// file to its owner, so RestrictToOwner is a no-op there. On Windows, a
// newly created file inherits its parent directory's ACL regardless of any
// mode argument passed to os.WriteFile/os.OpenFile — on a shared Windows
// host another local account can often still read it. RestrictToOwner
// applies an explicit, non-inherited DACL granting full access only to the
// file's owner, the same SDDL idiom other secret-bearing Go tools on
// Windows use (e.g. WireGuard for Windows).
package fsguard

import (
	"errors"
	"os"
)

// RestrictToOwner locks path down to its owning account on Windows (see the
// package doc) and is a no-op on POSIX. If path does not exist, this is
// treated as a no-op rather than an error: callers that opportunistically
// harden sidecar files (e.g. SQLite's -wal/-shm) which may not have been
// created yet should not have to special-case existence themselves.
func RestrictToOwner(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return restrictToOwner(path)
}
