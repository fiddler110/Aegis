//go:build !windows

package fsguard

// restrictToOwner is a no-op on POSIX platforms: the mode bit passed to
// os.WriteFile/os.OpenFile (e.g. 0o600) already restricts the file to its
// owner there (see fsguard_windows.go for why Windows needs an explicit ACL
// instead).
func restrictToOwner(path string) error { return nil }
