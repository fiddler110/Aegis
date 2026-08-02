package security

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// withReadCacheMetadata seams the single probe container run, mirroring
// withCacheFileExists — every P55.6 test runs without podman.
func withReadCacheMetadata(t *testing.T, fn func(ctx context.Context, rt sandbox.ContainerRuntime, image, script string) (string, error)) {
	t.Helper()
	orig := readCacheMetadata
	readCacheMetadata = fn
	t.Cleanup(func() { readCacheMetadata = orig })
}

// probeNow is the fixed "now" the age arithmetic is measured against, so a
// threshold test asserts the rule rather than the wall clock.
var probeNow = time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) int64 { return probeNow.Add(-d).Unix() }

// TestParseCacheProbe is the parser half of P55.6: the probe emits one
// tab-separated record per marker, and every shape it can emit — including the
// ones that mean "I don't know" — has to land as a distinct, non-silent state.
func TestParseCacheProbe(t *testing.T) {
	cases := []struct {
		name string
		out  string
		// want is keyed by tool; only the listed tools are asserted.
		want map[string]DBAge
	}{
		{
			name: "trivy metadata beats the file mtime",
			// The mtime record comes first, exactly as cacheProbeScript emits
			// it, so this also pins that metadata wins rather than whichever
			// record happened to be last.
			out: "mtime\ttrivy\t" + itoa(ago(48*time.Hour)) + "\n" +
				"meta\ttrivy\t{\"Version\":2,\"NextUpdate\":\"2026-08-03T13:11:51Z\",\"UpdatedAt\":\"2026-08-01T13:11:51Z\",\"DownloadedAt\":\"2026-08-02T15:02:05Z\"}\n",
			want: map[string]DBAge{"trivy": {
				Tool:      "trivy",
				UpdatedAt: time.Date(2026, 8, 1, 13, 11, 51, 0, time.UTC),
				Source:    "database build time",
			}},
		},
		{
			name: "mtime is the answer for tools that publish no metadata",
			out:  "mtime\tgrype\t" + itoa(ago(72*time.Hour)) + "\n",
			want: map[string]DBAge{"grype": {
				Tool:      "grype",
				UpdatedAt: time.Unix(ago(72*time.Hour), 0).UTC(),
				Source:    "downloaded to the cache",
			}},
		},
		{
			// Measured on the development machine: grype's DB was absent from
			// a cache volume that had trivy's and osv-scanner's, so "some
			// markers missing" is the real state, not a hypothetical.
			name: "a missing marker is Missing, not Unknown",
			out:  "missing\tgrype\n",
			want: map[string]DBAge{"grype": {Tool: "grype", Missing: true}},
		},
		{
			name: "unparseable metadata keeps the weaker mtime answer",
			out: "mtime\ttrivy\t" + itoa(ago(24*time.Hour)) + "\n" +
				"meta\ttrivy\tnot json at all\n",
			want: map[string]DBAge{"trivy": {
				Tool:      "trivy",
				UpdatedAt: time.Unix(ago(24*time.Hour), 0).UTC(),
				Source:    "downloaded to the cache",
			}},
		},
		{
			name: "a non-numeric mtime is Unknown, not zero-time",
			out:  "mtime\tosv-scanner\tnope\n",
			want: map[string]DBAge{"osv-scanner": {
				Tool:    "osv-scanner",
				Unknown: "the cache file has no readable modification time",
			}},
		},
		{
			// P11.1's no-silent-skip rule reaches the cache report too: a tool
			// the probe said nothing about must appear, flagged.
			name: "a tool the probe never mentioned is reported Unknown",
			out:  "mtime\ttrivy\t" + itoa(ago(time.Hour)) + "\n",
			want: map[string]DBAge{"grype": {
				Tool:    "grype",
				Unknown: "the cache probe reported nothing for this tool",
			}},
		},
		{
			name: "unrecognized lines are ignored rather than guessed at",
			out:  "garbage\nsomething\telse\tentirely\nmtime\tnot-a-scanner\t123\nmtime\ttrivy\t" + itoa(ago(time.Hour)) + "\n",
			want: map[string]DBAge{"trivy": {
				Tool:      "trivy",
				UpdatedAt: time.Unix(ago(time.Hour), 0).UTC(),
				Source:    "downloaded to the cache",
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCacheProbe(tc.out)
			if len(got) != len(multiscannerDBTools) {
				t.Fatalf("got %d entries, want one per DB-backed tool (%d)", len(got), len(multiscannerDBTools))
			}
			byTool := map[string]DBAge{}
			for _, a := range got {
				byTool[a.Tool] = a
			}
			for tool, want := range tc.want {
				g, ok := byTool[tool]
				if !ok {
					t.Fatalf("no entry for %q in %+v", tool, got)
				}
				if !g.UpdatedAt.Equal(want.UpdatedAt) || g.Source != want.Source || g.Missing != want.Missing || g.Unknown != want.Unknown {
					t.Errorf("%s = %+v, want %+v", tool, g, want)
				}
			}
		})
	}
}

// TestParseCacheProbeSorted keeps the report deterministic — it is rendered
// into a table an operator diffs across runs.
func TestParseCacheProbeSorted(t *testing.T) {
	got := parseCacheProbe("")
	for i := 1; i < len(got); i++ {
		if got[i-1].Tool >= got[i].Tool {
			t.Fatalf("entries are not sorted by tool: %v", got)
		}
	}
}

// TestDBAgeStaleThreshold pins DBStaleAfter's behavior at the boundary. The
// threshold's justification is a comment on the constant; this is the part
// that has to keep working: a daily/weekly refresh must never warn, and a
// cache populated once at provisioning time must.
func TestDBAgeStaleThreshold(t *testing.T) {
	cases := []struct {
		name string
		age  DBAge
		want bool
	}{
		{"fresh download", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(-6 * time.Hour)}, false},
		{"yesterday", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(-24 * time.Hour)}, false},
		{"a weekly refresh cadence must not flap", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(-DBStaleAfter + time.Hour)}, false},
		{"just past the threshold", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(-DBStaleAfter - time.Minute)}, true},
		{"provisioned once, months ago", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(-90 * 24 * time.Hour)}, true},
		// No database at all under-reports at least as badly as an old one.
		{"missing counts as stale", DBAge{Tool: "grype", Missing: true}, true},
		// But an age Aegis could not measure must not be *claimed* stale —
		// a warning nobody can act on is how the actionable ones get ignored.
		{"unknown does not count as stale", DBAge{Tool: "grype", Unknown: "no readable mtime"}, false},
		// A clock skewed into the future must read as fresh, not negative.
		{"future timestamp reads as fresh", DBAge{Tool: "trivy", UpdatedAt: probeNow.Add(48 * time.Hour)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.age.Stale(probeNow); got != tc.want {
				t.Errorf("Stale = %v, want %v (age %v)", got, tc.want, tc.age.Age(probeNow))
			}
		})
	}
}

// TestDatabaseAgeReportWarning covers the operator-facing string: it must name
// every stale tool, say why silence isn't proof of safety, and carry the one
// remedy — and must be empty when everything is current, since a warning that
// prints on a healthy install is a warning nobody reads.
func TestDatabaseAgeReportWarning(t *testing.T) {
	cases := []struct {
		name    string
		report  DatabaseAgeReport
		want    []string
		wantAny bool
	}{
		{
			name: "everything current is silent",
			report: DatabaseAgeReport{Tools: []DBAge{
				{Tool: "grype", UpdatedAt: probeNow.Add(-2 * time.Hour), Source: "downloaded to the cache"},
				{Tool: "trivy", UpdatedAt: probeNow.Add(-25 * time.Hour), Source: "database build time"},
			}},
		},
		{
			name:   "no multiscanner configured is silent",
			report: DatabaseAgeReport{NotConfigured: true},
		},
		{
			name:   "an unreadable cache does not manufacture a staleness claim",
			report: DatabaseAgeReport{Unavailable: "podman isn't available now"},
		},
		{
			name: "stale and missing are both named, with the remedy",
			report: DatabaseAgeReport{Tools: []DBAge{
				{Tool: "grype", Missing: true},
				{Tool: "osv-scanner", UpdatedAt: probeNow.Add(-3 * time.Hour)},
				{Tool: "trivy", UpdatedAt: probeNow.Add(-30 * 24 * time.Hour)},
			}},
			wantAny: true,
			want: []string{
				"grype (no database)", "trivy (30 days old)",
				"under-report", "aegis security update-db",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.report.Warning(probeNow)
			if !tc.wantAny {
				if got != "" {
					t.Fatalf("warning = %q, want silence", got)
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("warning = %q, want it to contain %q", got, want)
				}
			}
			if strings.Contains(got, "osv-scanner") {
				t.Errorf("warning = %q names a tool that is not stale", got)
			}
		})
	}
}

// TestFormatDatabaseAgesStaysSilentWhenNotConfigured is the not-nagging rule
// for P55.6: host-only scanning is supported, every host scanner manages its
// own database under its own rules, and Aegis has neither visibility into
// those nor standing to advise a container build on every status call.
func TestFormatDatabaseAgesStaysSilentWhenNotConfigured(t *testing.T) {
	if got := FormatDatabaseAges(DatabaseAgeReport{NotConfigured: true}, probeNow); got != "" {
		t.Fatalf("format = %q, want empty for a host-only install", got)
	}
	if got := FormatDatabaseAges(DatabaseAgeReport{}, probeNow); got != "" {
		t.Fatalf("format = %q, want empty when there is nothing to report", got)
	}
}

// TestFormatDatabaseAges checks the rendered table states each row's age and,
// for a stale row, marks it — the age alone is the number an operator has to
// interpret, and the whole item exists because nobody was interpreting it.
func TestFormatDatabaseAges(t *testing.T) {
	got := FormatDatabaseAges(DatabaseAgeReport{Tools: []DBAge{
		{Tool: "grype", Missing: true},
		{Tool: "osv-scanner", UpdatedAt: probeNow.Add(-3 * time.Hour), Source: "downloaded to the cache"},
		{Tool: "trivy", UpdatedAt: probeNow.Add(-30 * 24 * time.Hour), Source: "database build time"},
	}}, probeNow)

	for _, want := range []string{
		MultiscannerCacheVolume,
		"grype", "not downloaded", "aegis security update-db",
		"3 hours old", "downloaded to the cache",
		"30 days old", "database build time", "STALE",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table = %q, want it to contain %q", got, want)
		}
	}
	// The fresh row must not be marked; exactly one STALE marker.
	if n := strings.Count(got, "STALE"); n != 1 {
		t.Errorf("table has %d STALE markers, want 1:\n%s", n, got)
	}
}

// TestFormatDatabaseAgesUnavailable checks the "couldn't look" path renders as
// unknown rather than as a clean bill of health — the silent-all-clear shape
// this whole item exists to close.
func TestFormatDatabaseAgesUnavailable(t *testing.T) {
	got := FormatDatabaseAges(DatabaseAgeReport{Unavailable: "podman isn't available now"}, probeNow)
	if !strings.Contains(got, "unknown") || !strings.Contains(got, "podman isn't available now") {
		t.Fatalf("table = %q, want an unknown row carrying the reason", got)
	}
}

// TestCacheProbeScript pins the two properties of the generated shell that
// matter: it is derived from multiscannerDBTools (so a new DB-backed scanner
// gets an age report for free and the two can't disagree about the marker
// path), and it is ONE invocation. `aegis security status` already pays for a
// runtime detect, an image inspect and a per-tool cache probe; a per-tool age
// probe on top would be the change that makes operators stop running it.
func TestCacheProbeScript(t *testing.T) {
	script := cacheProbeScript()
	for tool, marker := range multiscannerDBTools {
		if !strings.Contains(script, marker) {
			t.Errorf("script does not stat %s's marker %q:\n%s", tool, marker, script)
		}
		if !strings.Contains(script, "'mtime\\t"+tool+"\\t") {
			t.Errorf("script emits no mtime record for %q:\n%s", tool, script)
		}
		if !strings.Contains(script, "'missing\\t"+tool+"\\n'") {
			t.Errorf("script emits no missing record for %q:\n%s", tool, script)
		}
	}
	// trivy is the only tool that publishes its data's own build time.
	if !strings.Contains(script, dbAgeMetadata["trivy"]) {
		t.Errorf("script does not read trivy's metadata.json:\n%s", script)
	}
	// Deterministic output: the script is embedded in a command line and
	// churn there would show up as spurious diffs in anything recording it.
	if script != cacheProbeScript() {
		t.Error("cacheProbeScript is not deterministic")
	}
}

// TestDatabaseAgesRunsExactlyOneContainer is the cost assertion, and it is a
// behavioral one rather than a comment: the probe must ask the runtime once
// for the whole cache, not once per tool.
func TestDatabaseAgesRunsExactlyOneContainer(t *testing.T) {
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})
	runs := 0
	withReadCacheMetadata(t, func(_ context.Context, _ sandbox.ContainerRuntime, _, _ string) (string, error) {
		runs++
		return "mtime\ttrivy\t" + itoa(ago(time.Hour)) + "\n", nil
	})

	rep := DatabaseAges(context.Background(), Options{Multiscanner: msPolicy(testImageID, "trivy")})
	if runs != 1 {
		t.Fatalf("probe ran %d containers, want exactly 1", runs)
	}
	if rep.Unavailable != "" || len(rep.Tools) != len(multiscannerDBTools) {
		t.Fatalf("report = %+v, want one entry per DB-backed tool", rep)
	}
}

// TestDatabaseAgesUnavailablePaths covers every way the probe can decline to
// answer. Each has to produce a distinct, actionable explanation rather than
// an empty report that reads like "all current".
func TestDatabaseAgesUnavailablePaths(t *testing.T) {
	cases := []struct {
		name          string
		policy        MultiscannerPolicy
		runtimeOK     bool
		inspectID     string
		probeErr      error
		wantNotConfig bool
		want          string
	}{
		{
			name:          "no multiscanner configured reports nothing at all",
			policy:        MultiscannerPolicy{},
			wantNotConfig: true,
		},
		{
			name:      "runtime down names the runtime that built the image",
			policy:    msPolicy(testImageID, "trivy"),
			runtimeOK: false,
			want:      "isn't available now",
		},
		{
			// Reading a timestamp out of an image that no longer matches the
			// pin would be reporting on something other than what scans run.
			name:      "image-ID mismatch refuses rather than reporting",
			policy:    msPolicy(testImageID, "trivy"),
			runtimeOK: true,
			inspectID: "sha256:9999999999999999999999999999999999999999999999999999999999999999",
			want:      "no longer matches",
		},
		{
			name:      "a failed probe container is reported, not swallowed",
			policy:    msPolicy(testImageID, "trivy"),
			runtimeOK: true,
			inspectID: testImageID,
			probeErr:  errors.New("exit status 125"),
			want:      "could not read the " + MultiscannerCacheVolume,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
				if !tc.runtimeOK {
					return "", false
				}
				return sandbox.RuntimePodman, true
			})
			withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
				return tc.inspectID, nil
			})
			withReadCacheMetadata(t, func(context.Context, sandbox.ContainerRuntime, string, string) (string, error) {
				return "", tc.probeErr
			})

			rep := DatabaseAges(context.Background(), Options{Multiscanner: tc.policy})
			if tc.wantNotConfig {
				if !rep.NotConfigured || rep.Unavailable != "" {
					t.Fatalf("report = %+v, want NotConfigured with no message", rep)
				}
				if FormatDatabaseAges(rep, probeNow) != "" || rep.Warning(probeNow) != "" {
					t.Error("a host-only install must produce no output at all")
				}
				return
			}
			if !strings.Contains(rep.Unavailable, tc.want) {
				t.Fatalf("unavailable = %q, want it to mention %q", rep.Unavailable, tc.want)
			}
			if len(rep.Tools) != 0 {
				t.Errorf("tools = %+v, want none when the cache could not be read", rep.Tools)
			}
		})
	}
}

// TestHumanDBAge keeps the rendered age readable at the resolution that
// matters for a database with a daily refresh cadence.
func TestHumanDBAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "under an hour"},
		{time.Hour, "1 hour"},
		{5 * time.Hour, "5 hours"},
		{47 * time.Hour, "47 hours"},
		{48 * time.Hour, "2 days"},
		{16 * 24 * time.Hour, "16 days"},
	}
	for _, tc := range cases {
		if got := humanDBAge(tc.d); got != tc.want {
			t.Errorf("humanDBAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// itoa keeps the probe-output fixtures above readable.
func itoa(v int64) string { return strconv.FormatInt(v, 10) }
