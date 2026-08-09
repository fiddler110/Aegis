package builtin

import (
	"strings"
	"testing"
)

// Each case here was emitted by a real model against the PowerShell shell tool
// during the P38.1 re-test, after reading a description that explicitly said
// Unix commands do not work.
func TestCheckPosixOnWindows(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string // substring the message must carry; "" means allowed
	}{
		{"posix tmp redirect", "python recon.py . > /tmp/recon_output.txt 2>&1", "/tmp"},
		{"posix home path", "cp out.txt /home/user/out.txt", "/home"},
		{"ls with unix flags", `ls -la "C:/repo/file.py"`, "short flags"},
		{"grep", "grep -rn PENDING .", "Select-String"},
		{"find", "find . -name '*.py'", "Get-ChildItem -Recurse"},
		{"grep after a pipe", "Get-Content x.txt | grep foo", "Select-String"},
		{"head after a semicolon", "Get-Location; head -5 x.txt", "Get-Content -TotalCount"},

		// Allowed: PowerShell-native, or a Unix name PowerShell aliases fine.
		{"powershell native", "Get-ChildItem -Force", ""},
		{"aliased ls without flags", "ls", ""},
		{"cat is aliased", "cat file.txt", ""},
		{"python with a windows path", `python C:/repo/recon.py C:/repo`, ""},
		// A path-looking string that is not one of the POSIX roots.
		{"relative path", "python ./scripts/recon.py .", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkPosixOnWindows(tc.cmd, true)
			if tc.want == "" {
				if got != "" {
					t.Errorf("command %q was refused: %s", tc.cmd, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("command %q\n got: %q\nwant it to mention %q", tc.cmd, got, tc.want)
			}
		})
	}
}

// The check is about which shell actually runs the command, not which OS the
// daemon is on: under a container backend the commands really are POSIX.
func TestCheckPosixOnWindowsSkippedWhenNotPowerShell(t *testing.T) {
	for _, cmd := range []string{"grep -rn x .", "ls -la", "cat /etc/hosts"} {
		if got := checkPosixOnWindows(cmd, false); got != "" {
			t.Errorf("command %q refused with a non-PowerShell executor: %s", cmd, got)
		}
	}
}
