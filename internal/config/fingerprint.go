package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// fingerprintVersion is mixed into every digest so the hash changes if the
// covered set or the encoding below ever changes. Bumping it re-prompts every
// trusted directory once, which is the correct outcome: a grant recorded under
// the old rule did not cover what the new rule covers.
const fingerprintVersion = "aegis-workspace-trust-v1\n"

// SecurityFingerprint hashes the security-relevant configuration a trust grant
// for dir would unlock (P66.25/SEC-07).
//
// # What it covers
//
// Exactly the keys of dir's own .aegis/config.yaml that P66.5's inverted freeze
// list does *not* mark projectSettable — i.e. the keys `aegis trust` exists to
// unlock: hooks, mcp, plugins, lsp, permission, sandbox, security, commands,
// server, provider (minus the named settable knobs), notify, git, workspace,
// search, embeddings, diagram, cleanup, data_dir, and anything added to Config
// later, since policyFor defaults an unlisted key to frozen. That default is
// what makes this honest without maintenance: a new dangerous key is
// fingerprinted the day it exists, not the day somebody remembers it.
//
// The two baselineOnly keys (data_dir, security.dast.allowed_targets) are in
// the covered set too, even though trust never unlocks them. A project that
// starts asking for them is still a project that changed in a security-relevant
// way, and the operator seeing that in the re-prompt is better than the digest
// carrying an exception nobody can remember the reason for.
//
// Only the *project file's own* keys are hashed — not the merged effective
// config. Hashing the merge would move the fingerprint when the operator edits
// their own ~/.config/aegis/config.yaml or exports an AEGIS_* variable, which
// re-prompts for a change the repository did not make. Hashing the project file
// alone means the digest moves when, and only when, project-controlled content
// moves.
//
// # What it deliberately does NOT cover: .aegis/.env
//
// The honest fingerprint would include .aegis/.env, and this one does not. The
// reason is ordering, and the ordering is P66.1/SEC-01's whole point: trust is
// resolved *before* any project-controlled file is read, so that .env is never
// parsed on the strength of a decision that has not been made yet. Covering
// .env in the fingerprint would mean reading and parsing project-controlled
// content ahead of the trust decision — reintroducing the exact ordering P66.1
// exists to prevent — so the two cannot both be had, and this is the smaller
// hole of the two:
//
//   - .env is read only in an *already trusted* workspace, so the gap is not a
//     path from "clone a repo" to "Aegis behaved differently"; it needs a
//     standing grant first.
//   - .env may not set AEGIS_* at all (loadDotEnv drops those keys and logs,
//     trusted or not). It is a secrets file, not a config layer, so an
//     unfingerprinted .env edit cannot change an Aegis setting the way an
//     unfingerprinted `hooks:` or `commands:` block can.
//
// What it can still do is set ordinary environment variables that a child
// process reads (PATH, GIT_SSH_COMMAND, NODE_OPTIONS, ...), so this is a real
// residual hole in a *previously* trusted workspace and is documented as one in
// docs/configuration.md under `aegis trust`, not quietly rounded off. Revoking
// trust (`aegis trust --revoke`) is the mitigation; TestDotEnvIsNotFingerprinted
// pins the behavior so that starting to cover it silently is also a test
// failure.
func SecurityFingerprint(dir string) string {
	h := sha256.New()
	h.Write([]byte(fingerprintVersion))
	for _, line := range securityRelevantConfigLines(dir) {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	// Always non-empty: workspacetrust.Check reads an empty fingerprint as a
	// pre-P66.25 grant, so the "no project config at all" case must still
	// produce a digest (of the empty key set) rather than "".
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// securityRelevantConfigLines renders dir's project config as sorted
// "key=value" lines, keeping only keys whose trust policy is not
// projectSettable.
//
// An unreadable or malformed file hashes as a sentinel rather than as the empty
// set: a file that cannot be parsed today but parsed yesterday is a change, and
// collapsing it onto "no config" would let a syntax error look like a matching
// fingerprint.
func securityRelevantConfigLines(dir string) []string {
	path := filepath.Join(dir, ".aegis", "config.yaml")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return []string{"\x00unparseable"}
	}
	var lines []string
	for key, val := range k.All() {
		if policyFor(key) == projectSettable {
			continue
		}
		lines = append(lines, key+"="+encodeFingerprintValue(val))
	}
	sort.Strings(lines)
	return lines
}

// encodeFingerprintValue renders one config value stably. JSON is used because
// it sorts object keys, so a map re-ordered by an editor does not move the
// digest, while a changed value always does.
func encodeFingerprintValue(v any) string {
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%#v", v)
}

// WorkspaceTrustFor answers the trust question for dir the one way every caller
// should ask it: against the current fingerprint (P66.25/SEC-07). Callers that
// only need a yes/no use WorkspaceTrusted; `aegis trust` and the startup
// warning use the Status so they can distinguish "never trusted" from "what you
// trusted has changed".
func WorkspaceTrustFor(dir string) workspacetrust.Status {
	return workspacetrust.Open(WorkspaceTrustStorePath()).Check(dir, SecurityFingerprint(dir))
}

// WorkspaceTrusted reports whether dir is trusted *and* its security-relevant
// config still matches the grant. A stale grant is not trust.
func WorkspaceTrusted(dir string) bool {
	return WorkspaceTrustFor(dir) == workspacetrust.Trusted
}

// TrustWorkspace records (or renews) a trust grant for dir against the content
// currently on disk. Every write path goes through here so no call site has to
// remember to compute the fingerprint — a Trust call that forgot would write a
// grant Check reads as permanently stale.
//
// This is the origin-less form: it exists for callers (mostly tests, and
// `aegis trust`, whose default already matches) that don't have a more
// specific interface to report. See TrustWorkspaceWithOrigin.
func TrustWorkspace(dir string) error {
	return TrustWorkspaceWithOrigin(dir, reqorigin.CLI)
}

// TrustWorkspaceWithOrigin is TrustWorkspace plus FIND-27's origin stamp:
// origin should be one of reqorigin's constants, naming the interface that
// made this trust decision (`aegis trust` on the CLI, the TUI's security
// config screen, an HTTP PATCH from the web UI, ...) so an inserted or
// hand-edited grant is at least visibly missing (or wrong) origin/MAC data
// rather than indistinguishable from a real one.
func TrustWorkspaceWithOrigin(dir, origin string) error {
	return workspacetrust.Open(WorkspaceTrustStorePath()).TrustWithOrigin(dir, SecurityFingerprint(dir), origin)
}
