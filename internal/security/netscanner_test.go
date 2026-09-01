package security

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// withLookPath seams the PATH probe, so "the container beat the host binary"
// can be asserted on a machine that has neither.
func withLookPath(t *testing.T, fn func(string) bool) {
	t.Helper()
	prev := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = prev })
}

func netPolicy(id string, tools ...string) NetscannerPolicy {
	p := NetscannerPolicy{
		Enabled:  true,
		Image:    NetscannerDefaultImage,
		ImageID:  id,
		Runtime:  sandbox.RuntimePodman,
		Tools:    map[string]bool{},
		Verified: true, // P81.13: tests using this factory exercise resolution, not the verify gate itself
		check:    &multiscannerCheck{},
	}
	if len(tools) == 0 {
		tools = NetscannerTools()
	}
	for _, t := range tools {
		p.Tools[t] = true
	}
	return p
}

// netOptions is an Options with the netscanner enabled and the tools the
// network path cares about explicitly on (nmap/nuclei are opt-in).
func netOptions(p NetscannerPolicy) Options {
	on := true
	tools := map[string]ToolPolicy{}
	for _, n := range []string{"nmap", "nuclei", "trivy", "grype", "dockle"} {
		tools[n] = ToolPolicy{Enabled: on, EnabledExplicit: true}
	}
	return Options{Tools: tools, Netscanner: p}
}

// TestNetscannerRunArgsNeverMountAWorkspace is the invariant the second image
// exists for. It runs with network on, so the only thing standing between a
// hostile repo and an exfiltration path is that the repo is never mounted —
// and the enforcement is that runNetscannerImage has no directory parameter to
// pass. This asserts the result: exactly one mount, the named cache volume.
func TestNetscannerRunArgsNeverMountAWorkspace(t *testing.T) {
	args := netscannerRunArgs(sandbox.RuntimePodman, NetscannerDefaultImage, "trivy", "image", "alpine:3.10")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--network bridge") {
		t.Errorf("the netscanner must have network — it is the reason it exists: %v", args)
	}
	mounts := []string{}
	for i, a := range args {
		if a == "-v" && i+1 < len(args) {
			mounts = append(mounts, args[i+1])
		}
	}
	if len(mounts) != 1 || mounts[0] != NetscannerCacheVolume+":"+netscannerCacheMount {
		t.Fatalf("netscanner mounts = %v, want exactly the cache volume", mounts)
	}
	// A bind mount is what a workspace mount would look like: a host path, not
	// a named volume. Nothing here may produce one.
	if strings.Contains(mounts[0], "/src") || strings.Contains(mounts[0], ":\\") {
		t.Errorf("netscanner mount looks like a host path: %q", mounts[0])
	}
	if strings.Contains(joined, MultiscannerCacheVolume) {
		t.Errorf("the networked image must not touch the offline scanners' database volume: %v", args)
	}
	if !strings.Contains(joined, "--cap-drop=ALL") || !strings.Contains(joined, "--security-opt=no-new-privileges") {
		t.Errorf("netscanner runs must drop privileges like every other scanner run: %v", args)
	}
}

// TestNetscannerCapabilitiesAreNarrow: only nmap gets anything back, and only
// NET_RAW. A capability list that quietly grew would undo the hardening the
// mount invariant is protecting.
func TestNetscannerCapabilitiesAreNarrow(t *testing.T) {
	for _, tool := range NetscannerTools() {
		args := strings.Join(netscannerRunArgs(sandbox.RuntimePodman, NetscannerDefaultImage, tool), " ")
		switch tool {
		case "nmap":
			if !strings.Contains(args, "--cap-add=NET_RAW") {
				t.Errorf("nmap needs NET_RAW or it silently degrades to a connect scan and refuses -O: %s", args)
			}
		default:
			if strings.Contains(args, "--cap-add") {
				t.Errorf("%s was granted a capability it has no stated need for: %s", tool, args)
			}
		}
	}
	if len(netscannerCaps) != 1 {
		t.Errorf("netscannerCaps = %v; every entry is a hardening exception and needs its own reason", netscannerCaps)
	}
}

// TestNetscannerReportPathKeepsCapabilities: the report-collecting path enters
// the container through `sh`, not through the tool. Keying the capability grant
// on the entry command instead of on the tool would drop nmap's NET_RAW with no
// error anywhere — the container would start, run, and quietly do less
// (connect scan instead of SYN, OS detection refused outright). That is the
// silent-degradation shape, so it gets its own assertion.
func TestNetscannerReportPathKeepsCapabilities(t *testing.T) {
	args := netscannerRunArgsFor(sandbox.RuntimePodman, NetscannerDefaultImage, "nmap", "sh",
		netscannerCollect("nmap", "/tmp/out.xml", []string{"-sV", "10.0.0.1"})...)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cap-add=NET_RAW") {
		t.Fatalf("nmap lost NET_RAW when routed through sh: %v", args)
	}
	// And the entry command really is sh, so this is not passing by accident.
	if !strings.Contains(joined, NetscannerDefaultImage+" sh -c") {
		t.Errorf("expected the container to be entered through sh: %v", args)
	}
	// nuclei goes through the same path and must NOT pick up a capability it
	// has no need for.
	nuc := strings.Join(netscannerRunArgsFor(sandbox.RuntimePodman, NetscannerDefaultImage, "nuclei", "sh",
		netscannerCollect("nuclei", "/tmp/out.sarif", []string{"-target", "x"})...), " ")
	if strings.Contains(nuc, "--cap-add") {
		t.Errorf("nuclei was granted a capability: %s", nuc)
	}
}

// TestNetscannerCollectPassesArgsAsPositionals: targets reach the container as
// positional parameters, never interpolated into the shell script, so a target
// string cannot become shell syntax.
func TestNetscannerCollectPassesArgsAsPositionals(t *testing.T) {
	args := netscannerCollect("nmap", "/tmp/out.xml", []string{"-sV", "10.0.0.1; rm -rf /"})
	if args[0] != "-c" {
		t.Fatalf("expected an `sh -c` form, got %v", args)
	}
	script := args[1]
	if strings.Contains(script, "10.0.0.1") || strings.Contains(script, "rm -rf") {
		t.Errorf("target was interpolated into the script: %q", script)
	}
	if !strings.Contains(script, `"$@"`) {
		t.Errorf("script must consume arguments as positionals, got %q", script)
	}
	if args[2] != "sh" {
		t.Errorf("missing the $0 placeholder before the positional args: %v", args)
	}
	// `;` rather than `&&`: a tool that dies outright must leave cat failing on
	// a missing report, which surfaces as a scan error rather than as an empty
	// (clean-looking) result.
	if strings.Contains(script, "&&") {
		t.Errorf("script uses && — a failed tool would then be reported as a clean scan: %q", script)
	}
}

// TestResolveNetworkPrefersTheContainer covers the P55.4 ordering carried onto
// this path: the pinned, ID-verified image beats an unpinned host binary.
func TestResolveNetworkPrefersTheContainer(t *testing.T) {
	withLookPath(t, func(string) bool { return true })
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})

	method, rt, image, reason := ResolveNetwork(context.Background(), "nmap", netOptions(netPolicy(testImageID)))
	if method != MethodContainer {
		t.Fatalf("nmap = %s (%s), want container", method, reason)
	}
	if rt != sandbox.RuntimePodman || image != NetscannerDefaultImage {
		t.Errorf("resolved to %s/%s, want podman/%s", rt, image, NetscannerDefaultImage)
	}
}

// TestResolveNetworkFallsBackToHost: a refused container must not fail the
// tool — an unpinned scan beats no scan — but the operator has to be told.
func TestResolveNetworkFallsBackToHost(t *testing.T) {
	withLookPath(t, func(string) bool { return true })
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return "", false
	})

	r := ResolveNetworkDetailed(context.Background(), "nmap", netOptions(netPolicy(testImageID)))
	if r.Method != MethodHost {
		t.Fatalf("nmap = %s (%s), want host fallback", r.Method, r.Reason)
	}
	if r.FallbackWhy == "" || !strings.Contains(r.Note, "netscanner") {
		t.Errorf("a silent fallback to an unpinned binary is the thing being avoided: note=%q why=%q", r.Note, r.FallbackWhy)
	}
}

// TestResolveNetworkIgnoresTheMultiscanner is the separation this split is for:
// the offline, workspace-mounted image must never be offered to a tool that
// needs to reach a remote target, no matter how it is configured.
func TestResolveNetworkIgnoresTheMultiscanner(t *testing.T) {
	withLookPath(t, func(string) bool { return false })
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})

	opts := netOptions(NetscannerPolicy{})            // netscanner off
	opts.Multiscanner = msPolicy(testImageID, "nmap") // multiscanner claims nmap
	opts.Multiscanner.Runtime = sandbox.RuntimePodman

	method, _, image, reason := ResolveNetwork(context.Background(), "nmap", opts)
	if method == MethodContainer {
		t.Fatalf("nmap resolved to %s — the --network none image cannot reach a target", image)
	}
	if !strings.Contains(reason, "--netscanner") {
		t.Errorf("the reason should point at the image that would fix this, got %q", reason)
	}
}

// TestResolveNetworkFailsClosedOnAnIDMismatch: the pin means the same thing on
// this image as on the other one.
func TestResolveNetworkFailsClosedOnAnIDMismatch(t *testing.T) {
	withLookPath(t, func(string) bool { return false })
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return "sha256:" + strings.Repeat("b", 64), nil
	})

	method, _, _, reason := ResolveNetwork(context.Background(), "nuclei", netOptions(netPolicy(testImageID)))
	if method != MethodNone {
		t.Fatalf("a rebuilt image must fail closed, got %s", method)
	}
	if !strings.Contains(reason, "no longer matches") {
		t.Errorf("reason = %q, want an ID-mismatch explanation", reason)
	}
}

// TestDockleNeverResolvesToAContainer: the one carve-out P55.7 leaves standing,
// and it must state the real reason rather than the generic egress one.
func TestDockleNeverResolvesToAContainer(t *testing.T) {
	withLookPath(t, func(string) bool { return false })
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimePodman, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})

	// Even if a hand-edited config claims the image carries dockle.
	p := netPolicy(testImageID, "dockle")
	method, _, _, reason := dockleScanner{}.Resolve(context.Background(), netOptions(p))
	if method == MethodContainer {
		t.Fatal("dockle cannot run from a container without the engine socket")
	}
	if !strings.Contains(reason, "engine socket") {
		t.Errorf("reason = %q, want the socket-privilege explanation", reason)
	}
}

// TestNetscannerToolsAllHaveVerification mirrors the multiscanner's guard: a
// tool added to the image but not to the verification table would be routed to
// the container and never checked.
func TestNetscannerToolsAllHaveVerification(t *testing.T) {
	for _, tool := range NetscannerTools() {
		exp, ok := netscannerCanaryExpectations[tool]
		if !ok {
			t.Errorf("the netscanner carries %q but netscannerCanaryExpectations has no entry", tool)
			continue
		}
		if len(exp.versionArgs) == 0 {
			t.Errorf("%q has no versionArgs — every tool gets at least a version probe", tool)
		}
		if exp.canarySkip == "" {
			if exp.minFindings < 1 {
				t.Errorf("%q expects %d findings; the point is asserting a NON-ZERO count", tool, exp.minFindings)
			}
			if _, ok := netscannerCanaryRunner(tool); !ok {
				t.Errorf("%q has a canary expectation but no runner", tool)
			}
		}
	}
	// dockle and zap are not in the image; nothing should claim otherwise.
	for _, absent := range []string{"dockle", "zap"} {
		for _, tool := range NetscannerTools() {
			if tool == absent {
				t.Errorf("%s must not be in the netscanner image — see its carve-out", absent)
			}
		}
	}
}

// TestNetworkFacingToolsAreAllKnown: the status table iterates this list, and a
// name with no descriptor would render a row saying "no scanner descriptor
// registered" to an operator who did nothing wrong.
// TestVerifyNetscannerImageRequiresVerified mirrors
// TestVerifyMultiscannerImageRequiresVerified for the netscanner path
// (P81.13).
func TestVerifyNetscannerImageRequiresVerified(t *testing.T) {
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})

	unverified := netPolicy(testImageID, "nmap")
	unverified.Verified = false
	if reason := verifyNetscannerImage(context.Background(), sandbox.RuntimePodman, unverified); reason == "" {
		t.Fatal("expected a pinned-but-unverified netscanner image to fail verifyNetscannerImage")
	}
	if reason := verifyNetscannerImageID(context.Background(), sandbox.RuntimePodman, unverified); reason != "" {
		t.Errorf("verifyNetscannerImageID (verify-image's own preflight) = %q, want \"\"", reason)
	}

	allowed := unverified
	allowed.AllowUnverified = true
	allowed.check = &multiscannerCheck{}
	if reason := verifyNetscannerImage(context.Background(), sandbox.RuntimePodman, allowed); reason != "" {
		t.Errorf("AllowUnverified = true: verifyNetscannerImage = %q, want \"\"", reason)
	}
}

func TestNetworkFacingToolsAreAllKnown(t *testing.T) {
	for _, name := range NetworkFacingTools() {
		if _, ok := DescriptorFor(name); !ok {
			t.Errorf("NetworkFacingTools names %q, which has no descriptor", name)
		}
	}
	// Everything the image carries has to be listed, or `security status` would
	// silently omit a tool that now has a container path.
	for _, tool := range NetscannerTools() {
		found := false
		for _, name := range NetworkFacingTools() {
			if name == tool {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is in the netscanner image but not in NetworkFacingTools, so status never shows it", tool)
		}
	}
}

// TestNetscannerPolicyFromConfigDefaultsToEveryTool: an enabled block with no
// tool list must not silently cover nothing.
func TestNetscannerPolicyFromConfigDefaultsToEveryTool(t *testing.T) {
	p := NetscannerPolicyFromConfig(config.NetscannerConfig{
		Enabled: true, Image: NetscannerDefaultImage, ImageID: testImageID,
	})
	for _, tool := range NetscannerTools() {
		if !p.Covers(tool) {
			t.Errorf("a tool-less config should cover %q, the same way the multiscanner's does", tool)
		}
	}
	if p.Covers("gitleaks") {
		t.Error("the netscanner must not claim a directory scanner")
	}
}

// TestNetscannerBuildRejectsTheMultiscannerTag: two images under one tag would
// leave whichever was built second answering for both pins.
func TestNetscannerBuildRejectsTheMultiscannerTag(t *testing.T) {
	_, err := BuildNetscanner(context.Background(),
		MultiscannerBuildOptions{Image: MultiscannerDefaultImage, Runtime: sandbox.RuntimePodman}, nil)
	if err == nil || !strings.Contains(err.Error(), "multiscanner's tag") {
		t.Fatalf("err = %v, want a refusal to reuse the multiscanner tag", err)
	}
}

// TestContainerfileCarriesTheNetscannerTarget: the netscanner is a --target
// stage in the multiscanner's build context, not a second context, so that both
// images share one fetch script, one set of pinned tool versions, and one
// source fingerprint. A rename of the stage would leave `build-image
// --netscanner` failing on a target the runtime can't find.
func TestContainerfileCarriesTheNetscannerTarget(t *testing.T) {
	data, err := multiscannerFS.ReadFile("multiscanner/Containerfile")
	if err != nil {
		t.Fatalf("read embedded Containerfile: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "AS "+netscannerBuildTarget) {
		t.Fatalf("Containerfile has no `AS %s` stage — `build-image --netscanner` would fail on --target", netscannerBuildTarget)
	}
	// The netscanner stage must come before `AS final`, or a default build
	// (which takes the last stage) would produce the wrong image entirely.
	if strings.Index(text, "AS "+netscannerBuildTarget) > strings.Index(text, "AS final") {
		t.Error("the netscanner stage is after `AS final`; a default `build-image` would build the netscanner instead of the multiscanner")
	}
	// Its cache mount is its own volume, and the environment says the opposite
	// of the multiscanner's about updating — this image has network, so it must
	// not inherit the skip-update flags that stop a fetch it can perform.
	netStage := text[strings.Index(text, "AS "+netscannerBuildTarget):]
	if strings.Contains(netStage, "TRIVY_SKIP_DB_UPDATE=true") {
		t.Error("the netscanner must not skip DB updates — it has network and no update-db step to fill its cache")
	}
	if strings.Contains(netStage, "WORKDIR /src") {
		t.Error("the netscanner has no /src and never will — see its header")
	}
}

// TestMultiscannerRunnerStillDeniesNetwork guards the other half of the split.
// The two runners are separate functions precisely so one can be hardened
// without the other quietly following; this asserts the workspace-mounted one
// did not pick up the networked one's posture.
func TestMultiscannerRunnerStillDeniesNetwork(t *testing.T) {
	args := containerRunArgs(sandbox.RuntimePodman, MultiscannerDefaultImage, "/work/repo", "gitleaks")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--network none") {
		t.Fatalf("the workspace-mounted runner must stay offline: %v", args)
	}
	if strings.Contains(joined, "--network bridge") {
		t.Fatalf("the two runners converged — a networked container can now see the workspace: %v", args)
	}
	if !strings.Contains(joined, ":/src") {
		t.Errorf("the multiscanner runner still mounts the workspace: %v", args)
	}
}
