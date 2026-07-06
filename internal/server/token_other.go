//go:build !windows

package server

// restrictToOwner is a no-op on POSIX platforms: os.WriteFile's 0o600 mode
// bit already restricts the file to its owner there (see token_windows.go
// for why Windows needs an explicit ACL instead).
func restrictToOwner(path string) error { return nil }
