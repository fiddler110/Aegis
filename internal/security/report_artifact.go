package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
)

// Where a scan report lands is a security decision, not a filing convention
// (P81.32 / FIND-32).
//
// A report is a curated map of a project's weaknesses: every finding's rule,
// severity and file:line, deduped and ranked so the interesting ones are at the
// top. It contains no source excerpt and no matched secret — the parsers store
// a tool's own rule description and a location, and the secret scanners store
// only their pre-redacted forms (see TestReportArtifactNeverEmitsAScannedCredential)
// — but the map itself is the exposure. Written under the scanned repository,
// it is one `git add -A` away from being pushed to wherever that repository is
// mirrored, by whoever next commits, without anyone deciding to publish it.
//
// So the default is the per-user data directory, which is outside every
// repository by construction, and the workspace is an explicit opt-in
// (InWorkspace). When an operator does opt in, the write also ensures a
// .gitignore entry covering the report directory and reports having done so,
// because the whole risk is the accidental commit and a silent mitigation is
// one nobody knows to rely on.

// ReportArtifactOption configures where WriteReportArtifact/ReportArtifactDir/
// ReportArtifactPath place a report. Variadic on purpose: every existing call
// site means "the default", and the default is the one that changed.
type ReportArtifactOption func(*reportArtifactPolicy)

type reportArtifactPolicy struct {
	inWorkspace bool
	dataDir     string
}

// InWorkspace selects the legacy in-repository location, <dir>/.aegis/security/.
// Explicit only — see the file comment for why it is no longer the default.
func InWorkspace() ReportArtifactOption {
	return func(p *reportArtifactPolicy) { p.inWorkspace = true }
}

// WithDataDir overrides the data directory reports are filed under, for a
// caller that has a resolved config.Config (cfg.DataDir) in hand or a test that
// needs the write confined to a temp dir. Without it the user-level default is
// used — deliberately not a config.Load() from inside a best-effort artifact
// writer, which would make where a report lands depend on the process's
// working directory.
func WithDataDir(dir string) ReportArtifactOption {
	return func(p *reportArtifactPolicy) { p.dataDir = dir }
}

func resolveReportPolicy(opts []ReportArtifactOption) reportArtifactPolicy {
	var p reportArtifactPolicy
	for _, opt := range opts {
		if opt != nil {
			opt(&p)
		}
	}
	if p.dataDir == "" {
		// config's own defaultDataDir is unexported; GlobalConfigPath is that
		// directory plus config.yaml, and is the stable exported way to ask.
		p.dataDir = filepath.Dir(config.GlobalConfigPath())
	}
	return p
}

// ReportArtifactDir is the directory a security scan of dir is persisted
// under, so a scan's findings survive the run that produced them instead of
// only existing in whatever ephemeral output captured it (terminal scrollback,
// a model turn).
//
// By default that is <dataDir>/security/reports/<slug>, where the slug names
// the scanned directory (its base name plus a hash of its absolute path) so
// two workspaces never overwrite each other's reports. With InWorkspace it is
// the historical <dir>/.aegis/security, parallel to SBOMArtifactPath's
// .aegis/sbom.cdx.json.
func ReportArtifactDir(dir string, opts ...ReportArtifactOption) string {
	p := resolveReportPolicy(opts)
	if p.inWorkspace {
		return workspaceReportDir(dir)
	}
	return filepath.Join(p.dataDir, "security", "reports", reportScopeSlug(dir))
}

// workspaceReportDir is the in-repository location, named separately because
// the .gitignore logic needs it independently of the active policy.
func workspaceReportDir(dir string) string {
	return filepath.Join(dir, ".aegis", "security")
}

// ReportArtifactPath is where WriteReportArtifact persists one named report
// (e.g. "scan", "image", "network", "dast") for dir. It must be resolved with
// the same options the write used, or a caller will report a path nothing was
// written to.
func ReportArtifactPath(dir, name string, opts ...ReportArtifactOption) string {
	return filepath.Join(ReportArtifactDir(dir, opts...), name+".json")
}

// reportScopeSlug identifies the scanned directory inside the shared data
// directory: a human-recognizable base name so an operator can find their
// report by eye, plus a short hash of the absolute path so two checkouts of
// the same repository — or two directories that merely share a name — never
// collide. Hash, not the full path flattened: the path is itself mildly
// sensitive (it names the operator's directory layout) and a data-dir listing
// is not the place to publish it.
func reportScopeSlug(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil || abs == "" {
		abs = filepath.Clean(dir)
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(abs)))
	base := sanitizeSlugComponent(filepath.Base(abs))
	if base == "" {
		base = "workspace"
	}
	return base + "-" + hex.EncodeToString(sum[:4])
}

// sanitizeSlugComponent keeps a base name to characters that are a valid path
// segment on every supported OS — a repository directory can be named things
// a Windows path will not accept.
func sanitizeSlugComponent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ReportArtifactWrite reports what WriteReportArtifact did, so a caller can
// tell an operator where the report went — which now matters, because by
// default it is nowhere they were looking — and whether the .gitignore entry
// the in-workspace posture depends on was actually written.
type ReportArtifactWrite struct {
	// Path is the file that was (or was meant to be) written.
	Path string
	// InWorkspace is true when the report was filed inside the scanned
	// repository rather than the data directory.
	InWorkspace bool
	// GitignoreUpdated is true when this write added the report directory to
	// the workspace's .gitignore. False when it was already covered, when the
	// scanned directory is not in a git repository, or when the report went to
	// the data directory (where nothing can commit it).
	GitignoreUpdated bool
	// Err is a non-nil write failure. Persisting a report is best-effort: the
	// findings are already in memory and on their way back to the caller, so a
	// failure here is reported, never fatal.
	Err error
}

// Note renders the one line a tool result or CLI should show about where the
// report went. Empty when nothing was written.
func (w ReportArtifactWrite) Note() string {
	if w.Path == "" {
		return ""
	}
	if w.Err != nil {
		return fmt.Sprintf("Report could not be written to %s: %v", w.Path, w.Err)
	}
	note := "Report written to " + w.Path
	if w.GitignoreUpdated {
		note += " (added to .gitignore — a scan report inside the repository is a map of its weaknesses, and must not be committed)"
	}
	return note
}

// WriteReportArtifact persists rep as JSON under ReportArtifactPath(dir, name,
// opts...), overwriting on every run — the same "keep the latest artifact, not
// a growing history" posture WriteSBOMArtifact already uses.
//
// Mode 0o600, not 0o644: the file is a ranked list of a project's exploitable
// weaknesses, and in the default data-directory location it sits beside the
// daemon token and the session database, which are 0o600 for the same reason.
//
// Best-effort — a write failure here must never fail the scan whose result is
// already held in memory and about to be returned to the caller — but the
// failure is returned rather than swallowed so a caller can say so.
func WriteReportArtifact(dir, name string, rep Report, opts ...ReportArtifactOption) ReportArtifactWrite {
	p := resolveReportPolicy(opts)
	out := ReportArtifactWrite{Path: ReportArtifactPath(dir, name, opts...), InWorkspace: p.inWorkspace}

	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		out.Err = err
		return out
	}
	if err := os.MkdirAll(filepath.Dir(out.Path), 0o700); err != nil {
		out.Err = err
		return out
	}
	if err := os.WriteFile(out.Path, data, 0o600); err != nil {
		out.Err = err
		return out
	}
	if p.inWorkspace {
		// After the write, not before: an ignore rule for a directory that was
		// never created is noise, and a failed write should not leave a
		// .gitignore edit behind claiming otherwise.
		updated, err := ensureReportGitignore(dir)
		out.GitignoreUpdated = updated
		if err != nil {
			out.Err = err
		}
	}
	return out
}

// reportGitignoreEntry is the line added to a scanned repository's .gitignore.
// Leading slash: anchored to the directory holding the .gitignore, so scanning
// a subdirectory of a repository ignores that subdirectory's reports and not,
// by accident, a same-named path elsewhere in the tree.
const reportGitignoreEntry = "/.aegis/security/"

// reportGitignoreCovering lists the entries that already cover the report
// directory. An operator who ignored all of .aegis/ has already made this
// decision, and appending a redundant rule to their file is worse than
// noticing.
var reportGitignoreCovering = []string{
	"/.aegis/security/", ".aegis/security/", "/.aegis/security", ".aegis/security",
	"/.aegis/", ".aegis/", "/.aegis", ".aegis", "/.aegis/*", ".aegis/*",
}

// ensureReportGitignore adds reportGitignoreEntry to dir's .gitignore when a
// report has been written inside a git repository and nothing already covers
// it. Reports true only when it actually appended a line.
//
// Scoped to an actual repository: outside one there is nothing to commit and
// so nothing to protect against, and creating a .gitignore in a directory the
// operator never version-controlled would be unexplained litter.
func ensureReportGitignore(dir string) (bool, error) {
	if !insideGitRepo(dir) {
		return false, nil
	}
	path := filepath.Join(dir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		for _, covering := range reportGitignoreCovering {
			if line == covering {
				return false, nil
			}
		}
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	if len(existing) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("# Aegis security scan reports: a ranked map of this repository's\n")
	b.WriteString("# weaknesses. Not for committing.\n")
	b.WriteString(reportGitignoreEntry + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// insideGitRepo walks up from dir looking for a .git entry (a directory in a
// normal clone, a file in a worktree or submodule).
func insideGitRepo(dir string) bool {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}
