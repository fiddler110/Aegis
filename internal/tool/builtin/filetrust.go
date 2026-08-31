package builtin

import (
	"fmt"

	"github.com/fiddler110/aegis/internal/trust"
)

// DR-1 — provenance for workspace file contents.
//
// docs/mcp-trust-boundary.md wraps MCP output, web fetches/searches, project
// and user personas and skills, the swarm mailbox, sub-agent results, memory
// entries and context files in a `<*_untrusted_content>` envelope. It did not
// wrap the highest-volume channel of all: the contents of files in the
// workspace, read through read_file and grep.
//
// The asymmetry was not defensible on threat grounds. The threat model already
// treats a cloned repository as untrusted — that premise is what
// internal/workspacetrust, enginecfg's persona-rule filtering and the
// skill/persona wrapping are built on — and a project's `persona.md` is wrapped
// because "a malicious dependency, template repo, or cloned project could plant
// one". A project's `src/handler.go` is planted by the same adversary in the
// same act and arrived unmarked. The asymmetry was defensible only on *cost*:
// file reads are the single highest-volume tool result and the envelope is
// ~150 bytes each, which is real budget under the local prompt profile.
//
// So the envelope is attached conditionally: run the heuristic scan that
// already exists (trust.ScanForInjection, the same one the `scan_tool_output`
// posture applies to the wrapped channels) and wrap only on a hit. The common
// case — ordinary source code — pays nothing at all, and the case that matters
// arrives marked, with the specific pattern that fired named inside the marker.
//
// This is a mitigation, not a boundary. The scan is coarse by construction and
// a file crafted to avoid it reaches the model unmarked, exactly as it did
// before. What it buys is that the *unsophisticated* case — the one that
// actually appears in planted READMEs and dependency files — is no longer the
// one channel that says nothing.

// markSuspiciousFileContent returns content wrapped in an untrusted-content
// envelope when the heuristic injection scan flags it, and returns it
// completely unchanged otherwise. sourceDesc completes "The content below was
// returned by <sourceDesc>."
func markSuspiciousFileContent(scan bool, sourceDesc string, attrs [][2]string, content string) string {
	if !scan || content == "" {
		return content
	}
	if len(trust.ScanForInjection(content)) == 0 {
		return content
	}
	return trust.Wrap("workspace_untrusted_content", attrs, sourceDesc, content, true)
}

// markSuspiciousFileRead is markSuspiciousFileContent for a single file read,
// naming the path so the model can see which file the warning is about.
func markSuspiciousFileRead(scan bool, path, content string) string {
	return markSuspiciousFileContent(scan,
		fmt.Sprintf("reading the workspace file %q", path),
		[][2]string{{"path", path}},
		content,
	)
}
