package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// withInspectImageID seams the runtime's image inspect so resolution can be
// tested without a real docker/podman install, mirroring withDetectRuntime.
func withInspectImageID(t *testing.T, fn func(ctx context.Context, rt sandbox.ContainerRuntime, image string) (string, error)) {
	t.Helper()
	orig := inspectImageID
	inspectImageID = fn
	t.Cleanup(func() { inspectImageID = orig })
}

// msPolicy builds an enabled policy for name, pinned to wantID.
func msPolicy(wantID string, tools ...string) MultiscannerPolicy {
	set := map[string]bool{}
	for _, tool := range tools {
		set[tool] = true
	}
	return MultiscannerPolicy{
		Enabled: true,
		Image:   MultiscannerDefaultImage,
		ImageID: wantID,
		Tools:   set,
		check:   &multiscannerCheck{},
	}
}

const testImageID = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// withCacheFileExists seams the containerized cache probe.
func withCacheFileExists(t *testing.T, present bool) {
	t.Helper()
	orig := cacheFileExists
	cacheFileExists = func(context.Context, sandbox.ContainerRuntime, string, string) bool { return present }
	t.Cleanup(func() { cacheFileExists = orig })
}

// TestResolveRefusesDBToolWithEmptyCache is the regression test for the worst
// failure this design could have. osv-scanner against an empty cache logs "no
// offline version of the OSV database is available" to stderr but still writes
// a valid empty JSON report to stdout — and since a scanner exiting non-zero
// with output is normal (that's how they report findings), Aegis reported
// "osv-scanner: 0 findings". A scanner that never opened a database must never
// look like a clean bill of health.
func TestResolveRefusesDBToolWithEmptyCache(t *testing.T) {
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})
	withCacheFileExists(t, false) // cache volume empty

	for _, name := range []string{"trivy", "osv-scanner"} {
		p := msPolicy(testImageID, name)
		p.Tools[name] = true
		opts := Options{
			Tools:        map[string]ToolPolicy{name: {Enabled: true, Method: "container"}},
			Multiscanner: p,
		}
		method, _, _, reason := Resolve(context.Background(), name, opts)
		if method != MethodNone {
			t.Errorf("%s: method = %v, want MethodNone against an empty cache (a DB-less run reports 0 findings)", name, method)
		}
		if !strings.Contains(reason, "update-db") {
			t.Errorf("%s: reason = %q, want it to point at `aegis security update-db`", name, reason)
		}
	}
}

// TestResolveAllowsDBToolWithPopulatedCache is the other half: once update-db
// has run, the same tools resolve normally.
func TestResolveAllowsDBToolWithPopulatedCache(t *testing.T) {
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})
	withCacheFileExists(t, true)

	p := msPolicy(testImageID, "trivy")
	opts := Options{
		Tools:        map[string]ToolPolicy{"trivy": {Enabled: true, Method: "container"}},
		Multiscanner: p,
	}
	method, _, _, reason := Resolve(context.Background(), "trivy", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q; want MethodContainer with a populated cache", method, reason)
	}
}

// TestCacheCheckOnlyAppliesToDBTools keeps the probe (a container run) off the
// scanners that don't need a database.
func TestCacheCheckOnlyAppliesToDBTools(t *testing.T) {
	withCacheFileExists(t, false)
	p := msPolicy(testImageID, "gitleaks")
	for _, name := range []string{"gitleaks", "opengrep", "hadolint"} {
		if reason := verifyMultiscannerCache(context.Background(), sandbox.RuntimePodman, name, p); reason != "" {
			t.Errorf("%s needs no DB but was gated on the cache: %q", name, reason)
		}
	}
	if reason := verifyMultiscannerCache(context.Background(), sandbox.RuntimePodman, "trivy", p); reason == "" {
		t.Error("trivy needs a DB and should have been gated on the empty cache")
	}
}

// TestResolveUsesMultiscannerImage is the core of the shared-image path: a
// tool with no image of its own, whose host binary is missing, resolves to the
// multiscanner image rather than MethodNone.
func TestResolveUsesMultiscannerImage(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})

	opts := Options{Multiscanner: msPolicy(testImageID, "test-ms")}
	method, rt, image, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q; want MethodContainer via the multiscanner", method, reason)
	}
	if image != MultiscannerDefaultImage {
		t.Errorf("image = %q, want %q", image, MultiscannerDefaultImage)
	}
	if rt != sandbox.RuntimePodman {
		t.Errorf("runtime = %q, want podman", rt)
	}
}

// TestResolveExplicitToolImageBeatsMultiscanner pins the precedence rule: an
// operator who pinned an image for one tool gets that image, not the shared
// one, even with the multiscanner enabled and covering that tool.
func TestResolveExplicitToolImageBeatsMultiscanner(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		t.Error("multiscanner image should not have been inspected when a per-tool image is pinned")
		return "", nil
	})

	const pinned = "ghcr.io/example/test-ms@sha256:abcdef0123456789"
	opts := Options{
		Tools:        map[string]ToolPolicy{"test-ms": {Enabled: true, Image: pinned}},
		Multiscanner: msPolicy(testImageID, "test-ms"),
	}
	method, _, image, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q; want MethodContainer", method, reason)
	}
	if image != pinned {
		t.Errorf("image = %q, want the explicitly pinned %q", image, pinned)
	}
}

// TestResolveMultiscannerNotCoveringTool proves the shared image is only used
// for tools it actually carries — a core-profile build must not be handed
// semgrep and asked to run it.
func TestResolveMultiscannerNotCoveringTool(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})

	// Policy covers some other tool, not test-ms.
	opts := Options{Multiscanner: msPolicy(testImageID, "some-other-tool")}
	method, _, _, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone (image doesn't carry this tool)", method)
	}
	if !strings.Contains(reason, "not installed") {
		t.Errorf("reason = %q, want the standard not-installed guidance", reason)
	}
}

// TestNoContainerImageReasonNamesTheProfileGap keeps the resolver from telling
// an operator to run the command they just ran. A core-profile image genuinely
// doesn't carry bandit; "run `aegis security build-image`" is a loop, whereas
// "rebuild with --profile full" is the actual fix.
func TestNoContainerImageReasonNamesTheProfileGap(t *testing.T) {
	opts := Options{Multiscanner: msPolicy(testImageID, "trivy")} // no bandit

	reason := noContainerImageReason("bandit", opts)
	if !strings.Contains(reason, "doesn't carry bandit") || !strings.Contains(reason, "--profile full") {
		t.Errorf("reason = %q, want it to name the profile gap and the fix", reason)
	}

	// With no multiscanner configured at all, the original generic advice is
	// still the right advice.
	generic := noContainerImageReason("bandit", Options{})
	if !strings.Contains(generic, "no container image configured") {
		t.Errorf("reason = %q, want the generic no-image guidance", generic)
	}
}

// TestResolveMultiscannerIDMismatch is the check that stands in for the
// digest-pin rule. An image rebuilt or retagged behind Aegis's back must fail
// closed with an actionable reason, never silently run.
func TestResolveMultiscannerIDMismatch(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return "sha256:2222222222222222222222222222222222222222222222222222222222222222", nil
	})

	opts := Options{Multiscanner: msPolicy(testImageID, "test-ms")}
	method, _, _, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone on an image-ID mismatch", method)
	}
	if !strings.Contains(reason, "no longer matches") || !strings.Contains(reason, "build-image") {
		t.Errorf("reason = %q, want a mismatch explanation pointing at build-image", reason)
	}
}

// TestResolveMultiscannerNotBuilt covers an enabled block with no recorded
// image ID — config edited by hand, or enabled before ever building.
func TestResolveMultiscannerNotBuilt(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})

	opts := Options{Multiscanner: msPolicy("", "test-ms")}
	method, _, _, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone when nothing was built", method)
	}
	if !strings.Contains(reason, "build-image") {
		t.Errorf("reason = %q, want guidance to run build-image", reason)
	}
}

// TestResolveMultiscannerImageAbsent covers the image being enabled and pinned
// but gone from the runtime's local storage (pruned, or a fresh machine).
func TestResolveMultiscannerImageAbsent(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return "", fmt.Errorf("no such image")
	})

	opts := Options{Multiscanner: msPolicy(testImageID, "test-ms")}
	method, _, _, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodNone {
		t.Fatalf("method = %v, want MethodNone when the image is absent", method)
	}
	if !strings.Contains(reason, "could not be verified") || !strings.Contains(reason, "build-image") {
		t.Errorf("reason = %q, want a verification-failed explanation pointing at build-image", reason)
	}
}

// TestResolveMultiscannerUsesRecordedRuntime is a regression test for a real
// failure: the image was built with podman, but resolution auto-detected and
// picked wslc (which DetectBest prefers on Windows), then reported the image
// missing from wslc's storage while it sat in podman's. A locally-built image
// is never pulled and exists only where it was built, so the recorded runtime
// has to win over auto-detection.
func TestResolveMultiscannerUsesRecordedRuntime(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true})

	var gotPriority []sandbox.ContainerRuntime
	withDetectRuntime(t, func(_ context.Context, priority []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		gotPriority = priority
		if len(priority) > 0 {
			return priority[0], true
		}
		return sandbox.RuntimeWSL, true // what DetectBest prefers on Windows
	})
	withInspectImageID(t, func(_ context.Context, rt sandbox.ContainerRuntime, _ string) (string, error) {
		if rt != sandbox.RuntimePodman {
			t.Errorf("inspected via %q, want the recording runtime podman", rt)
		}
		return testImageID, nil
	})

	p := msPolicy(testImageID, "test-ms")
	p.Runtime = sandbox.RuntimePodman
	method, rt, _, reason := Resolve(context.Background(), "test-ms", Options{Multiscanner: p})

	if method != MethodContainer {
		t.Fatalf("method = %v, reason = %q; want MethodContainer via podman", method, reason)
	}
	if rt != sandbox.RuntimePodman {
		t.Errorf("runtime = %q, want podman (the runtime that built the image)", rt)
	}
	if len(gotPriority) != 1 || gotPriority[0] != sandbox.RuntimePodman {
		t.Errorf("detect priority = %v, want [podman] only", gotPriority)
	}
}

// TestMultiscannerRuntimePriorityUnrecorded keeps the pre-multiscanner
// behavior for a config with no recorded runtime: auto-detect as before.
func TestMultiscannerRuntimePriorityUnrecorded(t *testing.T) {
	if p := (MultiscannerPolicy{}).RuntimePriority(); p != nil {
		t.Errorf("RuntimePriority = %v, want nil (auto-detect) when no runtime is recorded", p)
	}
	p := MultiscannerPolicy{Runtime: sandbox.RuntimeDocker}.RuntimePriority()
	if len(p) != 1 || p[0] != sandbox.RuntimeDocker {
		t.Errorf("RuntimePriority = %v, want [docker]", p)
	}
}

// TestResolveMultiscannerFallsBackToWSL proves a broken shared image doesn't
// preempt a working WSL path under "auto" — the image is one option among
// several, not a hard commitment.
func TestResolveMultiscannerFallsBackToWSL(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{
		Name: "test-ms", Binary: "definitely-not-a-real-binary", DefaultEnabled: true, WSLCapable: true,
	})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return "sha256:3333333333333333333333333333333333333333333333333333333333333333", nil
	})
	withWSLBinaryAvailable(t, func(context.Context, string, string) bool { return true })

	opts := Options{Multiscanner: msPolicy(testImageID, "test-ms")}
	method, _, _, reason := Resolve(context.Background(), "test-ms", opts)
	if method != MethodWSL {
		t.Fatalf("method = %v (reason %q), want MethodWSL when the image is stale but WSL works", method, reason)
	}
}

// TestResolveHostStillWinsUnderAuto guards the pre-existing contract: adding a
// shared image must not steal a run from an installed host binary.
func TestResolveHostStillWinsUnderAuto(t *testing.T) {
	withTestDescriptor(t, ScannerDescriptor{Name: "test-ms", Binary: "go", DefaultEnabled: true})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		t.Error("host binary is present; the multiscanner image should never be consulted")
		return "", nil
	})

	opts := Options{Multiscanner: msPolicy(testImageID, "test-ms")}
	method, _, _, _ := Resolve(context.Background(), "test-ms", opts)
	if method != MethodHost {
		t.Fatalf("method = %v, want MethodHost (go is on PATH)", method)
	}
}

// TestRunScannerImagePrependsBinaryOnlyForMultiscanner is the invocation-shape
// contract. A per-tool image entrypoints its own tool and takes bare args; the
// shared image has no entrypoint, so the binary name has to lead.
func TestRunScannerImagePrependsBinaryOnlyForMultiscanner(t *testing.T) {
	opts := Options{Multiscanner: msPolicy(testImageID, "trivy")}

	got := containerRunArgs(sandbox.RuntimePodman, MultiscannerDefaultImage, "/work", "trivy", "fs", "/src")
	if !argsContainInOrder(got, MultiscannerDefaultImage, "trivy", "fs", "/src") {
		t.Errorf("multiscanner args = %v, want the binary name right after the image", got)
	}
	if !opts.usesMultiscanner(MultiscannerDefaultImage) {
		t.Error("usesMultiscanner = false for the configured multiscanner image")
	}
	if opts.usesMultiscanner("ghcr.io/example/trivy@sha256:abc") {
		t.Error("usesMultiscanner = true for an unrelated per-tool image")
	}
	// Disabled policy must never claim an image, even a matching reference.
	off := Options{Multiscanner: MultiscannerPolicy{Image: MultiscannerDefaultImage}}
	if off.usesMultiscanner(MultiscannerDefaultImage) {
		t.Error("usesMultiscanner = true while the multiscanner is disabled")
	}
}

// TestWithCacheVolumeInsertsBeforeImage pins the mount's position: runtime
// flags must come before the image reference, or the runtime parses "-v ..."
// as arguments to the scanner instead of to itself.
func TestWithCacheVolumeInsertsBeforeImage(t *testing.T) {
	base := containerRunArgs(sandbox.RuntimePodman, MultiscannerDefaultImage, "/work", "trivy", "fs", "/src")
	got := withCacheVolume(base, MultiscannerDefaultImage)

	imageAt, mountAt := -1, -1
	for i, a := range got {
		switch {
		case a == MultiscannerDefaultImage && imageAt < 0:
			imageAt = i
		case a == MultiscannerCacheVolume+":"+multiscannerCacheMount:
			mountAt = i
		}
	}
	if mountAt < 0 {
		t.Fatalf("cache volume not mounted: %v", got)
	}
	if mountAt > imageAt {
		t.Errorf("cache mount at %d is after the image at %d — it would be passed to the scanner, not the runtime: %v", mountAt, imageAt, got)
	}
	if got[mountAt-1] != "-v" {
		t.Errorf("cache mount not preceded by -v: %v", got)
	}
	// The scanner's own arguments must survive intact after the image.
	if !argsContainInOrder(got, MultiscannerDefaultImage, "trivy", "fs", "/src") {
		t.Errorf("scanner args mangled: %v", got)
	}
}

// TestScanRunsStayNetworkNone is the guarantee that survives moving the
// databases out of the image: only `update-db` gets network. A scan mounts the
// workspace, so it must never also have outbound network.
func TestScanRunsStayNetworkNone(t *testing.T) {
	args := withCacheVolume(
		containerRunArgs(sandbox.RuntimePodman, MultiscannerDefaultImage, "/work", "trivy", "fs"),
		MultiscannerDefaultImage,
	)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--network none") {
		t.Errorf("scan run lost --network none: %v", args)
	}
}

func argsContainInOrder(args []string, want ...string) bool {
	i := 0
	for _, a := range args {
		if i < len(want) && a == want[i] {
			i++
		}
	}
	return i == len(want)
}

// TestContainerRunArgsHardeningUnchanged guards the flags the shared image
// inherits: --rm, no network, no capabilities, workspace at /src.
func TestContainerRunArgsHardeningUnchanged(t *testing.T) {
	args := containerRunArgs(sandbox.RuntimePodman, MultiscannerDefaultImage, "/work", "trivy", "fs")
	for _, want := range []string{"--rm", "--network", "none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "-w", "/src"} {
		found := false
		for _, a := range args {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in %v", want, args)
		}
	}
}

func TestNormalizeImageID(t *testing.T) {
	// podman reports a bare hex ID, docker prefixes sha256: — they must compare
	// equal, or every scan on one runtime would report a spurious mismatch.
	bare := "ABCDEF0123"
	prefixed := "sha256:abcdef0123"
	if normalizeImageID(bare) != normalizeImageID(prefixed) {
		t.Errorf("normalizeImageID(%q) = %q != normalizeImageID(%q) = %q",
			bare, normalizeImageID(bare), prefixed, normalizeImageID(prefixed))
	}
}

func TestMultiscannerToolsByProfile(t *testing.T) {
	core := MultiscannerTools(MultiscannerProfileCore)
	full := MultiscannerTools(MultiscannerProfileFull)

	if len(full) <= len(core) {
		t.Errorf("full profile (%d tools) should carry more than core (%d)", len(full), len(core))
	}
	has := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	if has(core, "semgrep") || has(core, "brakeman") || has(core, "nmap") {
		t.Errorf("core profile must not claim interpreter/network tools: %v", core)
	}
	if !has(full, "semgrep") || !has(full, "brakeman") || !has(full, "nmap") {
		t.Errorf("full profile is missing an expected tool: %v", full)
	}
	// No profile may claim an excluded tool. gosec is the one that matters
	// most here: it can't resolve Go packages without a toolchain and network,
	// and reports zero findings rather than failing when it can't — measured
	// at host 244 vs container 0 on this repo. Putting it back into the image
	// would ship a silent all-clear.
	for excluded := range multiscannerExcludedTools {
		if has(full, excluded) || has(core, excluded) {
			t.Errorf("%s is excluded (%s) but a profile claims it", excluded, multiscannerExcludedTools[excluded])
		}
	}
	if _, ok := multiscannerExcludedTools["gosec"]; !ok {
		t.Error("gosec must stay excluded: it silently reports 0 findings in a --network none container")
	}
}

// TestExcludedToolsExplainThemselves keeps the resolver from sending an
// operator round in a circle: for a tool no profile will ever carry, the
// reason must say why, not "rebuild with --profile full".
func TestExcludedToolsExplainThemselves(t *testing.T) {
	for name, why := range multiscannerExcludedTools {
		opts := Options{Multiscanner: msPolicy(testImageID, "trivy")}
		reason := noContainerImageReason(name, opts)
		if !strings.Contains(reason, "deliberately doesn't carry "+name) {
			t.Errorf("%s: reason = %q, want it to name the deliberate exclusion", name, reason)
		}
		if strings.Contains(reason, "--profile full") {
			t.Errorf("%s: reason suggests --profile full, but no profile carries it: %q", name, reason)
		}
		if why == "" {
			t.Errorf("%s: exclusion has no stated reason", name)
		}
	}
}

func TestMultiscannerPolicyFromConfig(t *testing.T) {
	p := MultiscannerPolicyFromConfig(config.MultiscannerConfig{
		Enabled: true, Image: "img", ImageID: "sha256:aa", Concurrency: 5, Tools: []string{"trivy", "gitleaks"},
	})
	if !p.Covers("trivy") || !p.Covers("gitleaks") {
		t.Error("policy should cover its configured tools")
	}
	if p.Covers("semgrep") {
		t.Error("policy should not cover a tool absent from its list")
	}
	if p.EffectiveConcurrency() != 5 {
		t.Errorf("EffectiveConcurrency = %d, want 5", p.EffectiveConcurrency())
	}

	// An enabled block with no tool list assumes the full profile rather than
	// covering nothing.
	empty := MultiscannerPolicyFromConfig(config.MultiscannerConfig{Enabled: true, Image: "img", ImageID: "sha256:aa"})
	if !empty.Covers("trivy") {
		t.Error("an empty tools list should fall back to the full profile set")
	}
	if empty.EffectiveConcurrency() != multiscannerDefaultConcurrency {
		t.Errorf("EffectiveConcurrency = %d, want the default %d", empty.EffectiveConcurrency(), multiscannerDefaultConcurrency)
	}

	// A disabled block must cover nothing regardless of its tool list.
	off := MultiscannerPolicyFromConfig(config.MultiscannerConfig{Image: "img", Tools: []string{"trivy"}})
	if off.Covers("trivy") {
		t.Error("a disabled multiscanner must cover nothing")
	}
}

// concurrencyFixtures reuses the recorded-output scanners the golden
// regression test drives, plus a scanner that always fails and one that's
// skipped, so the comparison below covers Ran, RanVia, Skipped and Findings
// rather than just the happy path.
func concurrencyFixtures() []Scanner {
	return []Scanner{
		sarifFixture("semgrep", "semgrep_sast.sarif.json", "semgrep"),
		sarifFixture("trivy-vuln", "trivy_vuln.sarif.json", "trivy"),
		sarifFixture("trivy-misconfig", "trivy_misconfig.sarif.json", "trivy"),
		fixtureScanner{name: "gitleaks", file: "gitleaks.json", parse: parseGitleaks},
		fixtureScanner{name: "trufflehog", file: "trufflehog.jsonl", parse: func(b []byte) ([]Finding, error) { return parseTrufflehog(b, true) }},
		fixtureScanner{name: "osv-scanner", file: "osv_scanner.json", parse: osvFixtureParse},
		sarifFixture("grype", "grype_sca.sarif.json", "grype"),
		sarifFixture("zap", "zap_dast.sarif.json", "zap"),
		fixtureScanner{name: "broken", file: "does_not_exist.json", parse: parseGitleaks},
	}
}

func runAtConcurrency(t *testing.T, dir string, n int) Report {
	t.Helper()
	opts := Options{Multiscanner: MultiscannerPolicy{Concurrency: n}}
	return RunWithOptions(context.Background(), dir, concurrencyFixtures(), opts)
}

// TestRunWithProgressConcurrencyIsDeterministic is the guarantee that makes
// parallel scanning safe to adopt: the Report must not depend on how many
// scanners ran at once, or on which finished first. Ran/RanVia are ordering-
// sensitive by nature, which is exactly why results are folded in plan order
// rather than as they complete.
func TestRunWithProgressConcurrencyIsDeterministic(t *testing.T) {
	dir := t.TempDir()

	sequential := runAtConcurrency(t, dir, 1)
	want, err := json.Marshal(sequential)
	if err != nil {
		t.Fatalf("marshal sequential report: %v", err)
	}
	if len(sequential.Findings) == 0 {
		t.Fatal("fixture set produced no findings — the comparison below would be vacuous")
	}

	// Repeat each level: a scheduling-order bug shows up intermittently, so a
	// single run at each concurrency could pass by luck.
	for _, n := range []int{2, 3, 8} {
		for attempt := 0; attempt < 5; attempt++ {
			got, err := json.Marshal(runAtConcurrency(t, dir, n))
			if err != nil {
				t.Fatalf("marshal report at concurrency %d: %v", n, err)
			}
			if string(got) != string(want) {
				t.Fatalf("concurrency %d (attempt %d) produced a different Report than sequential\n got: %s\nwant: %s", n, attempt, got, want)
			}
		}
	}
}

// TestRunWithProgressEmitsEveryScannerConcurrently proves the progress
// contract survives parallelism: every scanner still reports start+done (or
// skipped) exactly once, even though events now interleave.
func TestRunWithProgressEmitsEveryScannerConcurrently(t *testing.T) {
	dir := t.TempDir()
	scanners := concurrencyFixtures()

	var mu sync.Mutex
	phases := map[string][]ScanPhase{}
	opts := Options{Multiscanner: MultiscannerPolicy{Concurrency: 4}}
	RunWithProgress(context.Background(), dir, scanners, opts, func(ev ScanEvent) {
		// No lock needed if RunWithProgress serializes callbacks as documented
		// — this one is here so the race detector reports a broken contract
		// instead of corrupting the map.
		mu.Lock()
		defer mu.Unlock()
		phases[ev.Scanner] = append(phases[ev.Scanner], ev.Phase)
	})

	if len(phases) != len(scanners) {
		t.Errorf("got events for %d scanners, want %d", len(phases), len(scanners))
	}
	for _, sc := range scanners {
		got := phases[sc.Name()]
		if len(got) != 2 || got[0] != PhaseStart || got[1] != PhaseDone {
			t.Errorf("%s: phases = %v, want [start done]", sc.Name(), got)
		}
	}
}

// TestScanConcurrencyDefaults pins the sizing rules: unset means the built-in
// default, and an explicit 1 must restore strictly sequential execution for an
// operator who wants the old behavior back.
func TestScanConcurrencyDefaults(t *testing.T) {
	if got := (Options{}).scanConcurrency(); got != multiscannerDefaultConcurrency {
		t.Errorf("zero-value Options scanConcurrency = %d, want %d", got, multiscannerDefaultConcurrency)
	}
	one := Options{Multiscanner: MultiscannerPolicy{Concurrency: 1}}
	if got := one.scanConcurrency(); got != 1 {
		t.Errorf("scanConcurrency = %d, want 1 (sequential)", got)
	}
}

// TestSASTScanArgsForMultiscannerUsesBakedRules is a regression test for a
// real scan failure. "Pinned" registry packs (p/owasp-top-ten) are still
// fetched from semgrep.dev at scan time — pinning the name doesn't make them
// local — so opengrep exited 2 with a DNS resolution error inside a
// --network none container. Running from the multiscanner image must point
// --config at the packs baked into it instead.
func TestSASTScanArgsForMultiscannerUsesBakedRules(t *testing.T) {
	opts := Options{Multiscanner: msPolicy(testImageID, "opengrep")}

	got := sastScanArgsFor(opts, MultiscannerDefaultImage)
	joined := strings.Join(got, " ")
	for _, pack := range sastRulePacks {
		if strings.Contains(joined, " "+pack) {
			t.Errorf("args still reference the registry pack %q (needs network): %v", pack, got)
		}
		if !strings.Contains(joined, multiscannerRulePackPath(pack)) {
			t.Errorf("args missing the baked path for %q: %v", pack, got)
		}
	}

	// Any other image keeps the registry references — only the multiscanner
	// image is known to carry baked packs.
	other := sastScanArgsFor(opts, "ghcr.io/example/opengrep@sha256:abc")
	if !reflect.DeepEqual(other, sastScanArgs()) {
		t.Errorf("non-multiscanner args = %v, want the unmodified %v", other, sastScanArgs())
	}
}

// TestSelectScannersKeepsDefaultMethod is a regression test for selection
// silently changing execution method. `aegis scan --scanner semgrep` under
// security.default_method: container ran semgrep on the host, because
// SelectScanners created a tools entry with an empty Method and policyFor
// returns an existing entry verbatim rather than falling back to
// DefaultMethod. Picking *which* scanners run must not change *how* they run.
func TestSelectScannersKeepsDefaultMethod(t *testing.T) {
	all := []Scanner{semgrepScanner{}, trivyScanner{}}
	opts := Options{DefaultMethod: "container"}

	_, got, err := SelectScanners(all, opts, []string{"semgrep"})
	if err != nil {
		t.Fatalf("SelectScanners: %v", err)
	}
	if m := got.Tools["semgrep"].Method; m != "container" {
		t.Errorf("selected semgrep Method = %q, want the inherited \"container\"", m)
	}

	// An explicit per-tool method still wins — this fills a gap, never
	// overrides an operator's choice.
	opts2 := Options{
		DefaultMethod: "container",
		Tools:         map[string]ToolPolicy{"semgrep": {Method: "host"}},
	}
	_, got2, err := SelectScanners(all, opts2, []string{"semgrep"})
	if err != nil {
		t.Fatalf("SelectScanners: %v", err)
	}
	if m := got2.Tools["semgrep"].Method; m != "host" {
		t.Errorf("selected semgrep Method = %q, want the explicit \"host\" preserved", m)
	}
}

// TestMultiscannerContextCarriesEveryCopiedFile guards a mistake that only
// surfaces minutes into a build: the build context is materialized from the
// go:embed FS, so a file the Containerfile COPYs but the embed pattern omits
// fails with "no such file or directory" pointing at the context, not at the
// embed. Cheaper to catch here.
func TestMultiscannerContextCarriesEveryCopiedFile(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeMultiscannerContext(dir); err != nil {
		t.Fatalf("MaterializeMultiscannerContext: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Containerfile"))
	if err != nil {
		t.Fatalf("Containerfile not materialized: %v", err)
	}

	// Match `COPY <src> <dst>` for sources that aren't --from=stage copies.
	re := regexp.MustCompile(`(?m)^COPY\s+([^\s-][^\s]*)\s`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no plain COPY lines found — this test is no longer checking anything")
	}
	for _, m := range matches {
		src := m[1]
		if _, err := os.Stat(filepath.Join(dir, src)); err != nil {
			t.Errorf("Containerfile COPYs %q but it is not in the build context — add it to the go:embed pattern in multiscanner_build.go", src)
		}
	}
}

// TestUpdateDBScriptIsExecutableShell keeps the helper script materialized in a
// runnable shape; CRLF line endings in particular make it fail inside the
// Linux container with an opaque error.
func TestUpdateDBScriptIsExecutableShell(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeMultiscannerContext(dir); err != nil {
		t.Fatalf("MaterializeMultiscannerContext: %v", err)
	}
	for _, name := range []string{"fetch.sh", "update-db.sh"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(string(data), "#!/bin/sh") {
			t.Errorf("%s: missing #!/bin/sh shebang", name)
		}
		if strings.Contains(string(data), "\r\n") {
			t.Errorf("%s: has CRLF line endings, which break it inside a Linux container", name)
		}
	}
}

func TestMultiscannerRulePackPath(t *testing.T) {
	if got := multiscannerRulePackPath("p/owasp-top-ten"); got != "/opt/semgrep-rules/owasp-top-ten.yaml" {
		t.Errorf("multiscannerRulePackPath = %q, want the baked yaml path", got)
	}
}

// TestVerifyMultiscannerImageCaches proves the ~16 Resolve calls in one scan
// collapse to a single inspect, rather than shelling out per scanner.
func TestVerifyMultiscannerImageCaches(t *testing.T) {
	calls := 0
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		calls++
		return testImageID, nil
	})

	p := msPolicy(testImageID, "trivy")
	for i := 0; i < 5; i++ {
		if reason := verifyMultiscannerImage(context.Background(), sandbox.RuntimePodman, p); reason != "" {
			t.Fatalf("verify failed: %s", reason)
		}
	}
	if calls != 1 {
		t.Errorf("inspect called %d times across 5 verifies, want 1 (TTL cache)", calls)
	}
}
