// Binary-identity and tool-set trust for external MCP servers (P81.2/FIND-02).
//
// Two gaps this closes, both flowing from the same fact: an MCP server's
// identity in this system used to be nothing more than a configured command
// string. internal/workspacetrust already pins a *project's config content*
// (mcp.servers included) to a fingerprint, so a repository that adds or
// edits an mcp.servers entry re-prompts the operator — but nothing then
// checked that the *binary* PATH resolves to is the one the operator meant,
// or that the server's *advertised tool set* stayed within what was approved
// once it started talking. A shimmed/replaced binary on PATH, or a server
// that grows its tool set mid-session via tools/list_changed, both passed
// through unnoticed.
//
// ServerTrust is trust-on-first-use, deliberately: the operator already made
// the trust decision once, by adding this server to their own config (itself
// gated by workspacetrust for a project-sourced config). What this store adds
// is *drift* detection on top of that decision — the binary changing, or the
// tool surface growing — not a second approval prompt for the same act of
// configuring the server.
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// ServerTrust records what was approved for one stdio MCP server: the
// resolved binary's absolute path and content digest, and the tool names it
// was last seen advertising.
type ServerTrust struct {
	BinaryPath string    `json:"binary_path"`
	Digest     string    `json:"digest"` // hex SHA-256 of the resolved binary's contents
	ApprovedAt time.Time `json:"approved_at"`
	ToolNames  []string  `json:"tool_names"`
}

// TrustStore is a small JSON-backed map of per-server ServerTrust records,
// keyed by server name (config's mcp.servers[].name). Safe for concurrent use
// — mirrors internal/workspacetrust.Store's shape, keyed by server identity
// instead of by directory since a binary/tool-set grant is a property of the
// server, not of a filesystem path.
type TrustStore struct {
	mu      sync.Mutex
	path    string
	entries map[string]ServerTrust
}

// OpenTrustStore loads the store from path if it exists; a missing or
// unreadable file just starts empty, matching workspacetrust.Open's
// fail-closed posture (nothing is trusted yet, so the first connection to
// every server records a fresh baseline).
func OpenTrustStore(path string) *TrustStore {
	s := &TrustStore{path: path, entries: map[string]ServerTrust{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.entries)
	}
	return s
}

func (s *TrustStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	_ = fsguard.RestrictToOwner(s.path) // best-effort, same posture as workspacetrust
	return nil
}

// resolveBinaryDigest resolves command to an absolute path (via exec.LookPath,
// which is a no-op resolution when command is already absolute) and returns
// that path plus the hex SHA-256 digest of its current contents.
func resolveBinaryDigest(command string) (absPath, digest string, err error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", "", fmt.Errorf("resolve mcp server binary %q: %w", command, err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", fmt.Errorf("read mcp server binary %q: %w", resolved, err)
	}
	sum := sha256.Sum256(data)
	return resolved, hex.EncodeToString(sum[:]), nil
}

// CheckBinary resolves command's binary and checks it against server's
// recorded ServerTrust. A server seen for the first time is trusted and
// recorded (trust-on-first-use, per the package doc). A server whose
// resolved digest no longer matches what was recorded is refused — approving
// the new binary means removing or updating its entry in the trust store
// file (there is no interactive re-approval flow for a background daemon
// process to drive; this is the same "edit the file" recovery
// workspacetrust's own Stale grant requires calling Trust again for). ok is
// false for either a resolution failure or a digest mismatch; reason
// explains which.
func (s *TrustStore) CheckBinary(server, command string) (absPath string, ok bool, reason string) {
	absPath, digest, err := resolveBinaryDigest(command)
	if err != nil {
		return "", false, err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, known := s.entries[server]
	if !known {
		s.entries[server] = ServerTrust{BinaryPath: absPath, Digest: digest, ApprovedAt: time.Now().UTC()}
		if err := s.save(); err != nil {
			// Best-effort persistence: the in-memory record still governs this
			// process's own drift checks even if the write failed, so a full
			// disk doesn't also disable the protection.
			return absPath, true, fmt.Sprintf("trusted (unpersisted: %v)", err)
		}
		return absPath, true, "first connection: baseline recorded"
	}
	if entry.Digest != digest {
		return absPath, false, fmt.Sprintf(
			"binary digest changed since it was approved on %s (was %s, resolved to %s at %s) — "+
				"remove this server's entry from the mcp trust store to re-approve",
			entry.ApprovedAt.Format(time.RFC3339), entry.Digest, digest, absPath)
	}
	return absPath, true, ""
}

// CheckToolNames compares names (the tool set a server just advertised)
// against server's previously *approved* set (Approve must have been called
// at least once) and returns the subset that is new — grown beyond what was
// approved, per P81.2's third ask ("re-ask when it grows, not only when a
// schema changes"). Those names should not be exposed until the store is
// updated to approve them.
//
// nil ToolNames — no entry at all, or an entry CheckBinary just created and
// Approve has never touched — means there is no baseline to grow *from* yet,
// so nothing is reported as grown: the caller registers the current set and
// calls Approve to establish the baseline, the same trust-on-first-use
// CheckBinary applies to the binary itself. Once a real baseline exists, a
// name outside it stays held back until the store is edited (removing the
// server's entry re-baselines everything on the next connection, the same
// recovery CheckBinary's digest mismatch requires).
func (s *TrustStore) CheckToolNames(server string, names []string) (grown []string) {
	s.mu.Lock()
	entry, known := s.entries[server]
	s.mu.Unlock()
	if !known || entry.ToolNames == nil {
		return nil
	}
	for _, n := range names {
		if !slices.Contains(entry.ToolNames, n) {
			grown = append(grown, n)
		}
	}
	return grown
}

// FilterApproved partitions names (the tool set a server just advertised)
// into approved (safe to expose now — either already on the baseline, or
// there is no baseline yet so everything establishes one) and held (new
// since the last Approve call, and withheld from exposure until the store
// is updated). Callers register approved and pass exactly that list to
// Approve to update the baseline; held names are logged and left
// unregistered rather than silently exposed.
func (s *TrustStore) FilterApproved(server string, names []string) (approved, held []string) {
	grown := s.CheckToolNames(server, names)
	if len(grown) == 0 {
		return names, nil
	}
	heldSet := make(map[string]bool, len(grown))
	for _, n := range grown {
		heldSet[n] = true
	}
	for _, n := range names {
		if heldSet[n] {
			held = append(held, n)
			continue
		}
		approved = append(approved, n)
	}
	return approved, held
}

// Approve records names as the approved tool set for server, alongside
// whatever binary trust entry CheckBinary already created, and persists the
// store. Called once a tool set has been exposed (whether at initial
// registration or after a grown set is accepted) so the next
// CheckToolNames call has an up-to-date baseline.
func (s *TrustStore) Approve(server string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[server] // zero value if CheckBinary was never called (e.g. an HTTP server)
	entry.ToolNames = append([]string(nil), names...)
	s.entries[server] = entry
	return s.save()
}
