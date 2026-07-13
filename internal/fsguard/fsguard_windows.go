//go:build windows

package fsguard

import "golang.org/x/sys/windows"

// restrictToOwner applies an explicit, protected (non-inherited) DACL to
// path granting full access only to its owner (the account that created
// it). Without this, a file created under %APPDATA% (or a project's
// .aegis directory) inherits its parent directory's ACL, which commonly
// grants access to more than just the current user; os.WriteFile's mode
// argument has no effect on Windows ACLs at all. This is the same SDDL
// idiom other secret-bearing Go tools on Windows use (e.g. WireGuard for
// Windows) to lock a file to its owner: "D:" starts a DACL, "P" protects it
// from inheriting the parent's ACEs, "AI" marks it auto-inherited (required
// by the API), and the single ACE grants FA (full access) to OW (the
// special "owner rights" SID).
//
// The ACE carries OICI (object-inherit, container-inherit) flags. For a
// plain file this is a no-op — files have no children for the flags to
// apply to. It matters when path is a directory that already has children
// (e.g. FIND-20/P27.11 hardening the swarm mailbox `teams/` tree): setting a
// protected DACL on a directory makes Windows recompute every descendant
// that was relying on inherited ACEs from it, and a non-inheritable ACE here
// would leave those descendants with an empty, deny-everyone DACL —
// including the owner — rather than just failing to add new access. OICI
// makes the same owner-only grant propagate to existing children instead of
// orphaning them, and to new children created later.
func restrictToOwner(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;OICI;FA;;;OW)")
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
