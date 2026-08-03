package security

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// Verification for the second image (P55.7), built on the same two-step shape
// P55.3 established for the first: probe the version (catches absence), then run
// the tool against something known to be dirty and assert a non-zero finding
// count (catches everything else).
//
// The second step is why this file exists rather than a `--netscanner` flag on
// the existing table. The netscanner's tools cannot be canaried against a
// filesystem fixture — two of them take a network target and two take an image
// reference — so "something known to be dirty" is a different object here, and
// for half the tools it does not exist at all.

// netscannerCanaryImage is the reference trivy and grype are pointed at.
//
// It needs a *large, stable* CVE count, not merely a non-zero one, and picking
// it was not obvious. Measured against the built image:
//
//	alpine:3.14      trivy   0    grype  ~90
//	alpine:3.10      trivy   1    grype  121
//	debian:11-slim   trivy 190    grype  184
//
// The Alpine rows are the reason this constant carries a comment. Alpine's
// security data is per-branch and trivy stops reporting for a branch once it is
// out of support, so a tiny EOL Alpine — the obvious choice, and the one this
// started as — makes trivy report **zero on a working scanner**. That is
// precisely the signal this canary is built to mean "the scanner never loaded a
// database", so it would have turned a correct image into a failed gate. A
// margin of one (alpine:3.10) is no better: it is one upstream data change away
// from the same false failure.
//
// debian:11-slim has a wide margin for both tools and keeps it, because
// bullseye is an LTS release carrying a long tail of will-not-fix CVEs. It costs
// a ~30MB pull instead of ~3MB, which is the right trade for a check whose whole
// value is that a zero means something.
//
// Deliberately a tag rather than a digest, unlike every image reference Aegis
// *runs*. Nothing is executed from it — it is unpacked and inspected — and a
// digest pin here would make the canary fail whenever upstream re-pushed the
// tag, which is a maintenance failure reported as a broken image.
const netscannerCanaryImage = "debian:11-slim"

// netscannerCanaryFloor is the minimum finding count that counts as working.
//
// Higher than the multiscanner's floor of 1, deliberately. There, 1 is honest
// because a fixture with planted findings produces a handful at most. Here both
// tools report ~190 against the canary, so a result in single digits does not
// mean "detection got slightly worse" — it means something structural broke
// (partial database, a distro whose data was dropped), and a floor of 1 would
// wave that through.
const netscannerCanaryFloor = 20

// NetscannerCanaryImage is what verification scans, exported so the CLI can
// name it before the run instead of after — it is a registry pull, and an
// operator watching a silent command deserves to know what is being fetched.
func NetscannerCanaryImage() string { return netscannerCanaryImage }

// netscannerCanaryExpectations drives verification, in the same
// data-not-a-switch form as canaryExpectations. Every tool NetscannerTools
// returns must appear here (enforced by test).
var netscannerCanaryExpectations = map[string]canaryExpectation{
	"trivy": {versionArgs: []string{"--version"}, minFindings: netscannerCanaryFloor},
	"grype": {versionArgs: []string{"version"}, minFindings: netscannerCanaryFloor},
	// The same limitation nmap and nuclei have in the multiscanner, for the same
	// reason, and it does not go away just because this image has network:
	// canarying them would mean standing up a deliberately vulnerable listener
	// for Aegis to attack, which is a service to run rather than a fixture to
	// embed. A version probe is genuinely all that is available, and saying so
	// is better than counting it as a pass.
	"nmap":   {versionArgs: []string{"--version"}, canarySkip: "nmap needs a live network target; verifying it would mean standing up a service to scan, so only its version probe is checked here"},
	"nuclei": {versionArgs: []string{"-version"}, canarySkip: "nuclei needs a live network target; verifying it would mean standing up a service to scan, so only its version probe is checked here"},
}

// runNetscannerVersion is a seam over the containerized version probe, so the
// reporting around verification is testable without a container runtime.
var runNetscannerVersion = func(ctx context.Context, rt sandbox.ContainerRuntime, image, binary string, args ...string) (string, error) {
	cliArgs := netscannerRunArgs(rt, image, binary, args...)
	cmd := exec.CommandContext(ctx, string(rt), cliArgs...)
	// CombinedOutput for the same reason the multiscanner's probe uses it:
	// nuclei prints its banner to stderr, so a stdout-only read would report an
	// empty version for a working tool.
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, firstLine(text))
	}
	return firstVersionLine(text), nil
}

// netscannerCanaryRunnerFor is a seam over netscannerCanaryRunner.
var netscannerCanaryRunnerFor = netscannerCanaryRunner

// netscannerCanaryRunner resolves the counter for one tool, routing through the
// same ImageScanner implementation a real `aegis scan --image` uses — so a pass
// is evidence about the path Aegis actually takes, not about a parallel
// invocation that could drift from it.
func netscannerCanaryRunner(name string) (func(ctx context.Context, rt sandbox.ContainerRuntime, image string) (int, error), bool) {
	for _, sc := range DefaultImageScanners() {
		if sc.Name() != name {
			continue
		}
		scanner := sc
		return func(ctx context.Context, rt sandbox.ContainerRuntime, image string) (int, error) {
			findings, err := scanner.ScanImage(ctx, netscannerCanaryImage, MethodContainer, rt, image)
			if err != nil {
				return 0, err
			}
			return len(findings), nil
		}, true
	}
	return nil, false
}

// VerifyNetscanner runs the version probe and, where one is possible, the
// canary scan for every tool the configured netscanner image claims to carry.
//
// Same contract as VerifyMultiscanner: the error return is for conditions that
// make verification itself impossible (no image, no runtime, an ID mismatch),
// while a tool failing is data the caller reports and then decides an exit code
// from — one broken scanner must not hide the state of the others.
//
// Unlike the multiscanner's verification, this one needs working network: the
// canary pulls a small public image and the scanners refresh their own
// databases. That is inherent rather than incidental — an image whose entire
// purpose is registry and network egress cannot be verified offline, and a
// check that avoided the network would be verifying the wrong thing.
func VerifyNetscanner(ctx context.Context, p NetscannerPolicy, only []string, progress func(VerifyResult)) ([]VerifyResult, error) {
	if !p.Enabled || p.Image == "" {
		return nil, fmt.Errorf("no netscanner image configured — run `aegis security build-image --netscanner` first")
	}
	rt, ok := detectRuntime(ctx, p.RuntimePriority())
	if !ok {
		if p.Runtime != "" {
			return nil, fmt.Errorf("the netscanner image was built with %s, which isn't available now — start it (on Windows with Podman: `podman machine start`)", p.Runtime)
		}
		return nil, fmt.Errorf("no container runtime available (docker/podman)")
	}
	if reason := verifyNetscannerImage(ctx, rt, p); reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}

	names := make([]string, 0, len(p.Tools))
	for name, on := range p.Tools {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(only) > 0 {
		filtered, err := filterVerifyTools(names, only)
		if err != nil {
			return nil, err
		}
		names = filtered
	}

	results := make([]VerifyResult, 0, len(names))
	for _, name := range names {
		res := verifyOneNetscannerTool(ctx, rt, p, name)
		results = append(results, res)
		if progress != nil {
			progress(res)
		}
	}
	return results, nil
}

// verifyOneNetscannerTool is one row's worth of work.
func verifyOneNetscannerTool(ctx context.Context, rt sandbox.ContainerRuntime, p NetscannerPolicy, name string) VerifyResult {
	res := VerifyResult{Tool: name}

	exp, known := netscannerCanaryExpectations[name]
	if !known {
		res.Status = VerifyFail
		res.Detail = "no verification is defined for " + name + " in the netscanner image, so nothing here can establish that it works — add an entry to netscannerCanaryExpectations (internal/security/netscanner_verify.go)"
		return res
	}

	probeCtx, cancel := context.WithTimeout(ctx, canaryToolTimeout)
	version, err := runNetscannerVersion(probeCtx, rt, p.Image, name, exp.versionArgs...)
	cancel()
	if err != nil {
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("version probe `%s %s` failed inside %s (%v) — the tool is missing from the image or cannot start; rebuild with `aegis security build-image --netscanner`",
			name, strings.Join(exp.versionArgs, " "), p.Image, err)
		return res
	}
	res.Version = version

	if exp.canarySkip != "" {
		res.Status = VerifySkip
		res.Detail = exp.canarySkip
		return res
	}

	run, ok := netscannerCanaryRunnerFor(name)
	if !ok {
		res.Status = VerifyFail
		res.Detail = name + " has a canary expectation but no way to run it — netscannerCanaryRunner has no entry for it"
		return res
	}

	res.Expected = exp.minFindings
	scanCtx, cancel := context.WithTimeout(ctx, canaryToolTimeout)
	count, err := run(scanCtx, rt, p.Image)
	cancel()
	if err != nil {
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("ran but errored scanning %s: %v — this image needs working network access (a registry pull and a vulnerability-database refresh), so check that first", netscannerCanaryImage, err)
		return res
	}
	res.Findings = count
	if count < exp.minFindings {
		res.Status = VerifyFail
		res.Detail = fmt.Sprintf("expected at least %d finding(s) scanning %s, got %d — both scanners report roughly 190 there, so a result this low is a missing or partial vulnerability database rather than a clean image",
			exp.minFindings, netscannerCanaryImage, count)
		return res
	}
	res.Status = VerifyPass
	return res
}
