//go:build windows

package server

import "golang.org/x/sys/windows"

// restrictToOwner applies an explicit, protected (non-inherited) DACL to
// path granting full access only to its owner (the account that created
// it — the daemon process itself). Without this, a file created under
// %APPDATA% inherits its parent directory's ACL, which commonly grants
// access to more than just the current user; os.WriteFile's mode argument
// has no effect on Windows ACLs at all. This is the same SDDL idiom other
// secret-bearing Go tools on Windows use (e.g. WireGuard for Windows) to
// lock a file to its owner: "D:" starts a DACL, "P" protects it from
// inheriting the parent's ACEs, "AI" marks it auto-inherited (required by
// the API), and the single ACE grants FA (full access) to OW (the special
// "owner rights" SID).
func restrictToOwner(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;;FA;;;OW)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
}
