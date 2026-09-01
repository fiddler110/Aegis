package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// Vulnerability-database age reporting (P55.6).
//
// The multiscanner image deliberately disables every scanner's own staleness
// guard — GRYPE_DB_VALIDATE_AGE=false, TRIVY_SKIP_DB_UPDATE=true and friends —
// and that is correct: scans run --network none, so a tool that refuses to
// work with a cached database it can't refresh would simply not run at all.
// Air-gapped operation is a supported posture here.
//
// But suppressing the tools' warnings moved the responsibility for noticing an
// old database onto Aegis, and nothing picked it up. This is the pickup. A
// stale SCA database does not fail — it *under-reports*, so a scan against a
// three-month-old database is byte-for-byte indistinguishable from a clean
// repo. That is the same silent-all-clear shape as P55.1-P55.3, and it gets
// the same treatment: report it, name the remedy, change nothing else.
//
// Explicitly out of scope, and not by omission: this never auto-refreshes and
// never fails a scan. `aegis security update-db` stays the only container run
// given network access, and an operator who deliberately runs against a frozen
// database gets a line of text, not a broken toolchain.

// DBStaleAfter is how old a scanner's vulnerability data may get before status
// calls it out.
//
// Anchored on the tools' own refresh cadences rather than picked round: trivy
// rebuilds its database every 6 hours and stamps NextUpdate 24 hours out;
// grype publishes daily and its own max-allowed-built-age default is 5 days;
// osv-scanner's ecosystem zips are regenerated daily. So every tool here
// considers "current" to be measured in hours, and the loosest guard any of
// them ships is grype's 5 days.
//
// Seven days sits just past that, which is the point of the choice on both
// sides. An operator refreshing on any daily *or weekly* cadence never sees
// this warning, so it doesn't flap and doesn't get tuned out — and the case it
// does catch is the one that actually happens: a cache populated once at
// provisioning time and never again, which is weeks or months old by the time
// anyone asks whether the scan is trustworthy. A threshold at grype's own 5
// days would fire on a healthy weekly refresh; a month would let a whole CVE
// season pass in silence.
const DBStaleAfter = 7 * 24 * time.Hour

// DBAge is one scanner's vulnerability-database currency, as read out of the
// shared cache volume.
type DBAge struct {
	// Tool is the scanner name, matching multiscannerDBTools' keys.
	Tool string
	// UpdatedAt is when this tool's data was last refreshed. Zero when
	// Missing, or when Unknown explains why it couldn't be determined.
	UpdatedAt time.Time
	// Source names where UpdatedAt came from, because the two answers are not
	// equally strong and a reader should be able to tell them apart: trivy
	// publishes the data's own build time in metadata.json, while grype and
	// osv-scanner publish nothing usable offline, so their age is the cache
	// file's mtime — i.e. when Aegis downloaded it, a lower bound on how old
	// the data is rather than the data's own timestamp.
	Source string
	// Missing is true when the marker file isn't in the cache volume at all —
	// the tool has no database, which verifyMultiscannerCache already refuses
	// to scan with. Reported here too so `security status` shows the whole
	// cache in one place.
	Missing bool
	// Unknown is set when the probe ran but this tool's age couldn't be read
	// (an unparseable timestamp, say). Never conflated with Missing: "no
	// database" and "a database of unknown age" call for different actions.
	Unknown string
}

// Age is how old the data is relative to now. Zero for entries with no
// timestamp (Missing/Unknown) — callers check those first.
func (a DBAge) Age(now time.Time) time.Duration {
	if a.UpdatedAt.IsZero() {
		return 0
	}
	if d := now.Sub(a.UpdatedAt); d > 0 {
		return d
	}
	return 0
}

// Stale reports whether this entry is past DBStaleAfter. Missing counts as
// stale — a database that isn't there under-reports at least as badly as one
// that is old. Unknown does not: claiming staleness Aegis couldn't measure is
// the kind of wrong warning that gets the right ones ignored.
func (a DBAge) Stale(now time.Time) bool {
	if a.Missing {
		return true
	}
	if a.UpdatedAt.IsZero() {
		return false
	}
	return a.Age(now) > DBStaleAfter
}

// DatabaseAgeReport is DatabaseAges' result.
type DatabaseAgeReport struct {
	// Tools is one entry per DB-backed scanner, sorted by name. Empty when
	// Unavailable is set.
	Tools []DBAge
	// Unavailable explains why no age could be read: the runtime that built
	// the image isn't answering, the image doesn't verify, or the probe
	// container failed. Deliberately a string rather than an error return —
	// "how old is the data" having no answer is itself a fact worth printing,
	// not a failure of `aegis security status`.
	//
	// Empty alongside an empty Tools means there is nothing to say at all,
	// which is its own case and not a failure: see NotConfigured.
	Unavailable string
	// NotConfigured is true when there's no multiscanner image pinned, so
	// there is no shared database cache in the first place.
	//
	// It reports nothing rather than advising a build because host-only
	// scanning is a legitimate, supported posture, and every scanner on that
	// path manages its own database in its own directory under its own
	// staleness rules — which Aegis has neither visibility into nor standing
	// to override. Printing "unknown — run `aegis security build-image`" on
	// every status call to an operator who deliberately doesn't use the
	// container is the nagging that gets the real warnings skipped.
	NotConfigured bool
}

// Stale returns every entry past DBStaleAfter (or missing), sorted.
func (r DatabaseAgeReport) Stale(now time.Time) []DBAge {
	var out []DBAge
	for _, a := range r.Tools {
		if a.Stale(now) {
			out = append(out, a)
		}
	}
	return out
}

// Warning renders the one-line advisory for a report with stale entries, or ""
// when everything is current (or nothing could be read). Shared by every
// surface so the CLI, the TUI and the daemon can't drift on the remedy.
func (r DatabaseAgeReport) Warning(now time.Time) string {
	stale := r.Stale(now)
	if len(stale) == 0 {
		return ""
	}
	parts := make([]string, 0, len(stale))
	for _, a := range stale {
		if a.Missing {
			parts = append(parts, a.Tool+" (no database)")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s old)", a.Tool, humanDBAge(a.Age(now))))
	}
	return "vulnerability data is stale for " + strings.Join(parts, ", ") +
		" — these scanners under-report rather than fail against old data, so a scan can look clean because the database is behind; run `aegis security update-db` (the only step that needs network access)"
}

// FormatDatabaseAges renders the per-tool table `aegis security status` and
// the TUI's mirror of it both print, or "" when there is nothing to report —
// callers print it unconditionally and get silence on a host-only install.
func FormatDatabaseAges(r DatabaseAgeReport, now time.Time) string {
	if r.NotConfigured || (r.Unavailable == "" && len(r.Tools) == 0) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Vulnerability databases (%s volume):\n", MultiscannerCacheVolume)
	if r.Unavailable != "" {
		fmt.Fprintf(&b, "  unknown — %s\n", r.Unavailable)
		return b.String()
	}
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, a := range r.Tools {
		switch {
		case a.Missing:
			fmt.Fprintf(tw, "  %s\t-\tnot downloaded — run `aegis security update-db`\n", a.Tool)
		case a.Unknown != "":
			fmt.Fprintf(tw, "  %s\t-\tunknown — %s\n", a.Tool, a.Unknown)
		default:
			detail := fmt.Sprintf("%s (%s)", a.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC"), a.Source)
			if a.Stale(now) {
				detail += "  STALE"
			}
			fmt.Fprintf(tw, "  %s\t%s old\t%s\n", a.Tool, humanDBAge(a.Age(now)), detail)
		}
	}
	tw.Flush()
	return b.String()
}

// humanDBAge renders a duration at the resolution an operator cares about
// here. Sub-hour precision is meaningless for a database with a daily refresh
// cadence, and "156h0m0s" is not a thing anyone should have to divide by 24.
func humanDBAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "under an hour"
	case d < 48*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return strconv.Itoa(h) + " hours"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + " days"
	}
}

// dbAgeMetadata maps a scanner to a cache file carrying the data's own build
// timestamp, when it publishes one.
//
// Only trivy does. Its metadata.json holds UpdatedAt (when the database
// content was built), NextUpdate, and DownloadedAt; UpdatedAt is the honest
// answer to "how old is this data" — DownloadedAt only says when Aegis fetched
// it, which can be days newer than the content on a slow mirror.
//
// grype ships an import.json whose `source` URL happens to embed a build
// timestamp, and osv-scanner ships nothing at all, so both fall back to the
// marker file's mtime. That is a *download* time, not a build time — a lower
// bound on the data's age, which is the safe direction to be wrong in for a
// staleness check (it can under-report age, never invent it). Parsing a build
// date back out of a URL query string is not a contract grype documents, so
// it isn't done here.
var dbAgeMetadata = map[string]string{
	"trivy": "/cache/trivy/db/metadata.json",
}

// DatabaseAges reports how current each DB-backed scanner's data is, reading
// the cache volume in a **single** container run.
//
// One run is a requirement, not an optimization: `aegis security status`
// already pays for a runtime detect, an image inspect, and one cache probe per
// DB-backed tool, and a status command that takes noticeably longer than the
// scan it describes is a command operators stop running. The probe is one
// `sh -c` that stats every marker and cats trivy's metadata in one go.
func DatabaseAges(ctx context.Context, opts Options) DatabaseAgeReport {
	p := opts.Multiscanner
	if !p.Enabled || p.Image == "" {
		return DatabaseAgeReport{NotConfigured: true}
	}
	rt, ok := detectRuntime(ctx, p.RuntimePriority())
	if !ok {
		return DatabaseAgeReport{Unavailable: multiscannerNoRuntimeReason(opts)}
	}
	// Reuse the same image verification a scan gets (TTL-memoized, so this is
	// almost always free here): reading a timestamp out of an image whose ID
	// no longer matches the pin would be reporting on something other than
	// what scans actually run.
	if reason := verifyMultiscannerImageID(ctx, rt, p); reason != "" {
		return DatabaseAgeReport{Unavailable: reason}
	}
	out, err := readCacheMetadata(ctx, rt, p.Image, cacheProbeScript())
	if err != nil {
		return DatabaseAgeReport{Unavailable: fmt.Sprintf("could not read the %s volume via %s: %v", MultiscannerCacheVolume, rt, err)}
	}
	return DatabaseAgeReport{Tools: parseCacheProbe(out)}
}

// readCacheMetadata runs one probe container against the cache volume. A seam
// over the runtime, mirroring cacheFileExists — the mechanism P55.1 already
// established for looking inside the volume — so tests exercise the parsing
// and the thresholds without a container runtime.
var readCacheMetadata = func(ctx context.Context, rt sandbox.ContainerRuntime, image, script string) (string, error) {
	cmd := exec.CommandContext(ctx, string(rt), "run", "--rm", "--network", "none",
		"-v", MultiscannerCacheVolume+":"+multiscannerCacheMount,
		"--entrypoint=", image, "sh", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

// cacheProbeScript builds the shell the probe container runs. Generated from
// multiscannerDBTools rather than written out, so adding a DB-backed scanner
// there gets it an age report for free and the two can't disagree about which
// file is the marker.
//
// Output is one tab-separated record per line, which keeps the parser trivial
// and the failure mode visible (an unrecognized line is ignored, not guessed
// at):
//
//	mtime   <tool>  <unix-seconds>
//	missing <tool>
//	meta    <tool>  <metadata.json, newlines stripped>
//
// Newlines are stripped from the metadata blob because the record separator is
// the newline; trivy writes its metadata.json on one line today, but relying
// on that would be a parser that breaks on a formatting change in a file Aegis
// doesn't own.
func cacheProbeScript() string {
	tools := make([]string, 0, len(multiscannerDBTools))
	for name := range multiscannerDBTools {
		tools = append(tools, name)
	}
	sort.Strings(tools)

	var b strings.Builder
	for _, name := range tools {
		marker := multiscannerDBTools[name]
		fmt.Fprintf(&b, "if [ -e '%s' ]; then printf 'mtime\\t%s\\t%%s\\n' \"$(stat -c %%Y '%s')\"; else printf 'missing\\t%s\\n'; fi\n", marker, name, marker, name)
		meta, hasMeta := dbAgeMetadata[name]
		if !hasMeta {
			continue
		}
		fmt.Fprintf(&b, "if [ -f '%s' ]; then printf 'meta\\t%s\\t%%s\\n' \"$(tr -d '\\n\\r' < '%s')\"; fi\n", meta, name, meta)
	}
	return b.String()
}

// parseCacheProbe turns the probe's output into one DBAge per DB-backed tool,
// sorted. A tool the probe said nothing about is reported Unknown rather than
// omitted — P11.1's no-silent-skip rule applies to the cache report too.
func parseCacheProbe(out string) []DBAge {
	byTool := map[string]*DBAge{}
	entry := func(name string) *DBAge {
		if a, ok := byTool[name]; ok {
			return a
		}
		a := &DBAge{Tool: name}
		byTool[name] = a
		return a
	}
	for name := range multiscannerDBTools {
		a := entry(name)
		a.Unknown = "the cache probe reported nothing for this tool"
	}

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 {
			continue
		}
		kind, name := fields[0], fields[1]
		if _, known := multiscannerDBTools[name]; !known {
			continue
		}
		a := entry(name)
		switch kind {
		case "missing":
			*a = DBAge{Tool: name, Missing: true}
		case "mtime":
			if len(fields) < 3 {
				continue
			}
			secs, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
			if err != nil || secs <= 0 {
				*a = DBAge{Tool: name, Unknown: "the cache file has no readable modification time"}
				continue
			}
			*a = DBAge{Tool: name, UpdatedAt: time.Unix(secs, 0).UTC(), Source: "downloaded to the cache"}
		case "meta":
			if len(fields) < 3 || a.Missing {
				continue
			}
			// A parse failure here deliberately leaves the mtime answer in
			// place rather than downgrading the entry to Unknown: the weaker
			// signal is still a real lower bound on the data's age, and losing
			// it because a vendor changed a JSON field is how a staleness
			// check quietly stops checking.
			var md struct {
				UpdatedAt time.Time `json:"UpdatedAt"`
			}
			if err := json.Unmarshal([]byte(fields[2]), &md); err != nil || md.UpdatedAt.IsZero() {
				continue
			}
			a.UpdatedAt, a.Source, a.Unknown = md.UpdatedAt.UTC(), "database build time", ""
		}
	}

	out2 := make([]DBAge, 0, len(byTool))
	for _, a := range byTool {
		out2 = append(out2, *a)
	}
	sort.Slice(out2, func(i, j int) bool { return out2[i].Tool < out2[j].Tool })
	return out2
}
