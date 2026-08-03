package security

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// P55.7: a second locally-built image, split by MOUNT POSTURE rather than by
// tool category.
//
// Six scanners had no container path at all, and reconContainerFallbackUnsupported
// / imageContainerFallbackUnsupported both stated the reason the same way: they
// need network egress, and punching a hole through the scanner hardening wasn't
// done. The consequence on Windows was absurd — nmap and nuclei sat *inside* the
// multiscanner image the operator had already built, routed through WSL, so
// running them meant provisioning a Kali distro for two tools already on disk.
//
// Reviewing what each tool actually needs shows the split is not
// offline-vs-network. It is what the container is allowed to SEE:
//
//	trivy image, grype <ref>   registry egress    — no workspace (takes a ref)
//	nmap, nuclei               egress to target   — no workspace (takes targets)
//	zap                        egress to target   — no workspace (takes a URL)
//	dockle                     engine socket      — no workspace (inspects an image)
//	gosec                      module-proxy egress — NEEDS the workspace
//	trufflehog --verify        provider-API egress — NEEDS the workspace
//
// Only the last two want the workspace and the network at once, which is
// precisely the combination the hardening forbids and rightly so: workspace plus
// egress is an exfiltration path out of a hostile repo. (gosec's answer is
// P55.8's two-phase split; trufflehog --verify stays host-only and is off by
// default.) Everything above them needs egress but has nothing to steal, because
// it scans a remote target rather than local source.
//
// So: one image run with network on and **no workspace mount, ever**. The
// invariant is enforceable rather than conventional — runNetscannerImage below
// takes a target argument and has no directory parameter to pass. The existing
// --network none + workspace-mounted runner stays exactly as it is, and the two
// runners never converge.
//
// Two carve-outs, both deliberate:
//
//   - ZAP is already solved and stays as-is. dast.go runs it from the official
//     zaproxy image with its own /zap/wrk mount contract, so it already requires
//     no host install; folding a large Java app into a locally-built image buys
//     nothing.
//   - dockle needs the container engine socket, which is a *third* privilege
//     axis — socket access is effectively host root, not merely egress. Whether
//     Aegis should mount a container socket at all deserves an explicit
//     decision, not arrival as a side effect of this item, so dockle stays
//     host-only and says so.

const (
	// NetscannerDefaultImage is the tag `aegis security build-image
	// --netscanner` applies. "localhost/" marks it local-only, so a runtime
	// never treats a missing image as a registry pull — same reasoning as
	// MultiscannerDefaultImage.
	NetscannerDefaultImage = "localhost/aegis-netscanner:v1"

	// NetscannerCacheVolume holds trivy's and grype's vulnerability databases
	// for the network-facing image.
	//
	// Deliberately NOT MultiscannerCacheVolume, and the reason is the posture
	// split this whole file is about. This image has network, so it refreshes
	// its own databases on demand and needs no `update-db` step; the
	// multiscanner's cache is filled by exactly one privileged run and read by
	// runs that cannot fetch. Sharing one volume would put a networked writer
	// inside the cache that every offline scan reads, and a second named volume
	// costs nothing.
	NetscannerCacheVolume = "aegis-netscanner-cache"
	// netscannerCacheMount must match the cache paths the netscanner stage of
	// the Containerfile sets (TRIVY_CACHE_DIR, GRYPE_DB_CACHE_DIR, XDG_CACHE_HOME).
	netscannerCacheMount = "/cache"

	// netscannerTemplatesDir is where the netscanner stage bakes the pinned
	// nuclei template set. Because it is baked, the container path needs no
	// security.tools.nuclei.templates_version and no host-side git clone.
	netscannerTemplatesDir = "/opt/nuclei-templates"

	// netscannerBuildTarget is the Containerfile stage `build-image
	// --netscanner` selects. Both images are built from one context on purpose:
	// one fetch.sh, one set of pinned tool versions, one source fingerprint.
	netscannerBuildTarget = "netscanner"
)

// netscannerTools are the scanners this image carries. There is no profile
// split: the image is small (no Python/Ruby stacks, no vulnerability databases)
// and every tool in it shares one posture, which is the entire selection rule.
var netscannerTools = []string{"grype", "nmap", "nuclei", "trivy"}

// NetscannerTools returns the scanners the netscanner image carries, sorted.
func NetscannerTools() []string {
	out := append([]string(nil), netscannerTools...)
	sort.Strings(out)
	return out
}

// NetworkFacingTools are the scanners whose availability is decided by
// ResolveNetwork rather than Resolve: they analyze a remote target (a host
// list, a registry reference) instead of a directory.
//
// It exists because `aegis security status` used to resolve all sixteen
// descriptors through the directory resolver, which reported nmap as "not
// installed" on a machine where the tool was sitting in an image Aegis had
// built — the resolver it asked simply had no opinion about the network path.
// trivy and grype appear both here and among the directory scanners, which is
// honest rather than a duplicate: they genuinely have two paths with two
// postures, and one can work while the other doesn't.
func NetworkFacingTools() []string {
	return []string{"dockle", "grype", "nmap", "nuclei", "trivy", "zap"}
}

// netscannerCaps maps a tool to the Linux capabilities it cannot do its job
// without, on top of the --cap-drop=ALL every Aegis container run starts from.
//
// Only nmap has an entry, and only NET_RAW. A port scanner that cannot open a
// raw socket silently degrades: nmap falls back to a TCP connect scan and
// refuses OS detection outright ("requires root privileges"), so active mode
// (security.dast.allow_active) would fail on the capability rather than on
// anything the operator did. NET_RAW is narrow — it is the ability to craft
// packets on the container's own network namespace, not the engine-socket
// access dockle would need, which is why one is granted here and the other is
// a separate decision.
var netscannerCaps = map[string][]string{
	"nmap": {"NET_RAW"},
}

// NetscannerPolicy is the resolver's view of the network-facing image,
// translated from config.NetscannerConfig. Deliberately a smaller shape than
// MultiscannerPolicy: there is no profile, no concurrency knob (recon and image
// scans are driven one target at a time), and no database-cache precondition,
// because this image can fill its own cache.
type NetscannerPolicy struct {
	Enabled bool
	Image   string
	ImageID string
	// SourceFingerprint is the build-context hash recorded at build time. Both
	// images are built from the same embedded context, so this is comparable
	// against the same MultiscannerSourceFingerprint the multiscanner uses.
	SourceFingerprint string
	// Runtime is the container runtime that built the image — a locally-built
	// image lives only in that engine's storage. See RuntimePriority.
	Runtime sandbox.ContainerRuntime
	Tools   map[string]bool

	// check memoizes the image-ID verification for multiscannerVerifyTTL, the
	// same way MultiscannerPolicy.check does. Nil is valid and means "verify on
	// every call" — the shape a test constructing Options directly gets.
	check *multiscannerCheck
}

// Covers reports whether name should resolve to the network-facing image.
func (n NetscannerPolicy) Covers(name string) bool {
	return n.Enabled && n.Image != "" && n.Tools[name]
}

// RuntimePriority is the runtime probe order for the netscanner image: the one
// that built it, if recorded, and nothing else — for exactly the reason
// MultiscannerPolicy.RuntimePriority gives.
func (n NetscannerPolicy) RuntimePriority() []sandbox.ContainerRuntime {
	if n.Runtime == "" {
		return nil
	}
	return []sandbox.ContainerRuntime{n.Runtime}
}

// NetscannerPolicyFromConfig translates the user-facing config block.
func NetscannerPolicyFromConfig(cfg config.NetscannerConfig) NetscannerPolicy {
	p := NetscannerPolicy{
		Enabled:           cfg.Enabled,
		Image:             strings.TrimSpace(cfg.Image),
		ImageID:           strings.TrimSpace(cfg.ImageID),
		SourceFingerprint: strings.TrimSpace(cfg.SourceFingerprint),
		Runtime:           sandbox.ContainerRuntime(strings.TrimSpace(cfg.Runtime)),
		Tools:             map[string]bool{},
		check:             &multiscannerCheck{},
	}
	names := cfg.Tools
	if len(names) == 0 {
		// An enabled block with no tool list predates or bypasses build-image
		// (which always writes one). Assume everything the image is known to
		// carry: a tool it turns out to lack fails its own scan loudly with the
		// container's own error, which is the opposite of a silent skip.
		names = NetscannerTools()
	}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			p.Tools[n] = true
		}
	}
	return p
}

// verifyNetscannerImage confirms the image answering to policy.Image is the one
// build-image recorded. Same check, and the same reasoning, as
// verifyMultiscannerImage: a locally-built image has no registry digest to pin,
// so its real ID is compared instead of a regex on a reference string.
func verifyNetscannerImage(ctx context.Context, rt sandbox.ContainerRuntime, p NetscannerPolicy) string {
	if p.ImageID == "" {
		return "security.netscanner.enabled is true but no image has been built yet (security.netscanner.image_id is empty) — run `aegis security build-image --netscanner`"
	}
	key := "net|" + string(rt) + "|" + p.Image + "|" + p.ImageID
	if c := p.check; c != nil {
		c.mu.Lock()
		if c.key == key && time.Since(c.at) < multiscannerVerifyTTL {
			fail := c.fail
			c.mu.Unlock()
			return fail
		}
		c.mu.Unlock()
	}

	fail := ""
	actual, err := inspectImageID(ctx, rt, p.Image)
	switch {
	case err != nil:
		fail = fmt.Sprintf("netscanner image %s could not be verified in %s's local image storage (%v) — if it was removed, re-run `aegis security build-image --netscanner`", p.Image, rt, err)
	case normalizeImageID(actual) != normalizeImageID(p.ImageID):
		fail = fmt.Sprintf("netscanner image %s no longer matches the ID recorded in config (have %s, expected %s) — it was rebuilt or retagged; re-run `aegis security build-image --netscanner` to re-pin it", p.Image, shortImageID(actual), shortImageID(p.ImageID))
	}

	if c := p.check; c != nil {
		c.mu.Lock()
		c.key, c.at, c.fail = key, time.Now(), fail
		c.mu.Unlock()
	}
	return fail
}

// NetscannerSourceDrift reports whether the pinned netscanner image was built
// from a different Containerfile than the one this binary carries. Same
// advisory-not-fatal posture as MultiscannerSourceDrift, and the same source,
// since both images come out of one build context.
func NetscannerSourceDrift(p NetscannerPolicy) string {
	if !p.Enabled || p.Image == "" || p.ImageID == "" || p.SourceFingerprint == "" {
		return ""
	}
	current := MultiscannerSourceFingerprint()
	if current == "" || strings.EqualFold(current, p.SourceFingerprint) {
		return ""
	}
	return fmt.Sprintf("the netscanner image %s was built from an older Containerfile (recorded source %s, current %s) — it may be missing scanners or fixes added since; re-run `aegis security build-image --netscanner`",
		p.Image, shortImageID(p.SourceFingerprint), shortImageID(current))
}

// netscannerNoRuntimeReason explains that the runtime which built the image
// isn't answering. Mirrors multiscannerNoRuntimeReason.
func netscannerNoRuntimeReason(p NetscannerPolicy) string {
	return "the netscanner image was built with " + string(p.Runtime) + ", which isn't available now — start it (on Windows with Podman: `podman machine start`), or re-run `aegis security build-image --netscanner` to rebuild with an available runtime"
}

// ResolveNetwork is the availability resolver for the network-facing tools —
// the ones driven by ScanImage (a container image reference) and RunRecon (a
// target host list), as opposed to Resolve's directory scanners.
//
// It is a separate entry point rather than a branch inside Resolve, and that
// separation is the point. MethodContainer means two different things on the
// two paths: the workspace-mounted, --network none multiscanner over there, and
// the workspace-free, networked netscanner here. Folding them into one function
// would make "container" ambiguous at every call site and put one bad
// refactoring between the two postures this split exists to keep apart.
func ResolveNetwork(ctx context.Context, name string, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string) {
	return resolveNetwork(ctx, name, opts, nil)
}

// ResolveNetworkDetailed is ResolveNetwork with Resolution.Note/FallbackWhy
// attached, for callers that surface the advisory (`aegis security status`).
func ResolveNetworkDetailed(ctx context.Context, name string, opts Options) Resolution {
	var extra Resolution
	method, rt, image, reason := resolveNetwork(ctx, name, opts, &extra)
	extra.Method, extra.Runtime, extra.Image, extra.Reason = method, rt, image, reason
	return extra
}

func resolveNetwork(ctx context.Context, name string, opts Options, extra *Resolution) (Method, sandbox.ContainerRuntime, string, string) {
	d, ok := descriptors[name]
	if !ok {
		return MethodNone, "", "", name + ": no scanner descriptor registered"
	}
	policy := opts.policyFor(name, d.DefaultEnabled)
	if !policy.Enabled {
		if _, explicit := opts.Tools[name]; explicit {
			return MethodNone, "", "", "disabled by configuration (security.tools." + name + ".enabled: false)"
		}
		return MethodNone, "", "", "opt-in tool, not enabled by default — set security.tools." + name + ".enabled: true (or `aegis security config`) to turn it on"
	}

	// A per-tool security.tools.<name>.image is deliberately not consulted here.
	// It configures an image that scans a *directory*; running one of these
	// tools against a remote target out of an operator's own scanner image would
	// need a mount contract this code has never had, and silently reusing that
	// setting would be a guess about intent. The netscanner is the only container
	// path on this side.
	covered := opts.Netscanner.Covers(name)
	hostBroken := d.HostBroken[hostGOOS]
	hostAvailable := lookPath(d.Binary) && hostBroken == ""

	switch strings.ToLower(strings.TrimSpace(policy.Method)) {
	case "host":
		if hostAvailable {
			return MethodHost, "", "", ""
		}
		if hostBroken != "" {
			return MethodNone, "", "", hostBroken + " (security.tools." + name + ".method is \"host\")"
		}
		return MethodNone, "", "", d.Binary + " not installed on PATH (security.tools." + name + ".method is \"host\", no container fallback)"
	case "container":
		if !covered {
			return MethodNone, "", "", netscannerNotCoveredReason(name, opts)
		}
		rt, ok := opts.detectRuntime(ctx, opts.Netscanner.RuntimePriority())
		if !ok {
			return MethodNone, "", "", netscannerNoRuntimeReason(opts.Netscanner)
		}
		if why := verifyNetscannerImage(ctx, rt, opts.Netscanner); why != "" {
			return MethodNone, "", "", why
		}
		return MethodContainer, rt, opts.Netscanner.Image, ""
	case "wsl":
		if !d.WSLCapable {
			return MethodNone, "", "", name + " has no WSL execution path wired (security.tools." + name + ".method is \"wsl\")"
		}
		if wslBinaryAvailable(ctx, d.Binary, opts.WSLDistro) {
			return MethodWSL, "", "", ""
		}
		return MethodNone, "", "", d.Binary + " not found inside WSL (security.tools." + name + ".method is \"wsl\") — run `aegis security install " + name + "` to install it there"
	}

	// "auto": container first, then host, then WSL — the same ordering P55.4
	// established for directory scanners, and for the same reason. The pinned,
	// ID-verified image with a known tool version beats whatever is on PATH; a
	// refused container falls through rather than failing the tool, because an
	// unpinned scan still beats no scan.
	containerFallbackWhy := ""
	if covered {
		if rt, ok := opts.detectRuntime(ctx, opts.Netscanner.RuntimePriority()); ok {
			if why := verifyNetscannerImage(ctx, rt, opts.Netscanner); why == "" {
				return MethodContainer, rt, opts.Netscanner.Image, ""
			} else {
				containerFallbackWhy = why
			}
		} else {
			containerFallbackWhy = netscannerNoRuntimeReason(opts.Netscanner)
		}
	}
	if hostAvailable {
		if containerFallbackWhy != "" && extra != nil {
			extra.Note = name + " will run from the host binary, which is unpinned (whatever version is on PATH) and unconfined: the netscanner container was preferred but is unavailable — " + containerFallbackWhy
			extra.FallbackWhy = containerFallbackWhy
		}
		return MethodHost, "", "", ""
	}
	if d.WSLCapable && wslBinaryAvailable(ctx, d.Binary, opts.WSLDistro) {
		return MethodWSL, "", "", ""
	}
	if containerFallbackWhy != "" {
		return MethodNone, "", "", containerFallbackWhy
	}
	hostUnavailable := d.Binary + " not installed"
	if hostBroken != "" {
		hostUnavailable = hostBroken
	}
	return MethodNone, "", "", hostUnavailable + ", and " + netscannerNotCoveredReason(name, opts)
}

// netscannerNotCoveredReason explains why a network-facing tool has no
// container path. The three cases are genuinely different actions for the
// operator, so they are three different sentences rather than one generic one.
func netscannerNotCoveredReason(name string, opts Options) string {
	if why, excluded := multiscannerExcludedTools[name]; excluded {
		return "no Aegis scanner image carries " + name + ": " + why
	}
	if !opts.Netscanner.Enabled || opts.Netscanner.Image == "" {
		return "the network-facing scanner image hasn't been built — run `aegis security build-image --netscanner` (it runs with network access and no workspace mounted, which is what lets " + name + " reach a remote target at all)"
	}
	return "the netscanner image (" + opts.Netscanner.Image + ") doesn't carry " + name + " — it carries: " + strings.Join(NetscannerTools(), ", ")
}

// runNetscannerImage runs one tool out of the network-facing image.
//
// Note what this signature does NOT have: a directory. That absence is the
// enforcement of this image's invariant — there is no workspace parameter to
// pass, so no future call site can mount one into a networked container by
// accident. The multiscanner's runner (runScannerImage) keeps its dir parameter
// and its --network none, and the two never meet.
func runNetscannerImage(ctx context.Context, rt sandbox.ContainerRuntime, image, tool string, args ...string) ([]byte, error) {
	return runContainerCLI(ctx, rt, image, netscannerRunArgs(rt, image, tool, args...))
}

// runNetscannerReport runs a tool that insists on writing its report to a file
// and returns the report's bytes.
//
// With nothing mounted there is no shared file to read back, so the report is
// written inside the container and cat'd out — the pattern gitleaks already
// uses in the multiscanner.
//
// Note tool is passed through separately from the container's entrypoint, which
// is `sh` on this path. Collapsing the two would silently drop the tool's
// capability grant: nmap's NET_RAW is keyed on "nmap", and keying it on the
// entry command instead would leave nmap degraded to a connect scan with no
// error anywhere — the container would start, run, and quietly do less.
func runNetscannerReport(ctx context.Context, rt sandbox.ContainerRuntime, image, tool, reportPath string, args []string) ([]byte, error) {
	cliArgs := netscannerRunArgsFor(rt, image, tool, "sh", netscannerCollect(tool, reportPath, args)...)
	return runContainerCLI(ctx, rt, image, cliArgs)
}

// netscannerRunArgs builds the runtime command line for a tool invoked
// directly, where the binary and the capability key are the same name.
func netscannerRunArgs(rt sandbox.ContainerRuntime, image, tool string, args ...string) []string {
	return netscannerRunArgsFor(rt, image, tool, tool, args...)
}

// netscannerRunArgsFor is the general form: capsTool decides the capability
// grant, entry is what the container actually executes. Split out from the run
// so the properties that matter — network on, no bind mount, minimum
// capabilities — are unit-testable without a container runtime.
func netscannerRunArgsFor(rt sandbox.ContainerRuntime, image, capsTool, entry string, args ...string) []string {
	cliArgs := []string{"run", "--rm",
		// Network ON. Unlike the multiscanner, that is not an exception here —
		// it is the reason this image exists. What makes it safe is the mount
		// list below, which is one named cache volume and nothing else.
		"--network", "bridge",
	}
	cliArgs = append(cliArgs, sandbox.OCIHardeningFlags(rt)...)
	if sandbox.SupportsCapAdd(rt) {
		for _, c := range netscannerCaps[capsTool] {
			cliArgs = append(cliArgs, "--cap-add="+c)
		}
	}
	cliArgs = append(cliArgs, "-v", NetscannerCacheVolume+":"+netscannerCacheMount, image, entry)
	return append(cliArgs, args...)
}

// netscannerCollect builds the `sh -c '<script>' sh "$@"` invocation that runs
// a report-writing tool and cats its report to stdout.
//
// The positional-parameter form is deliberate: the tool's arguments are passed
// as `"$@"` rather than interpolated into the script, so a target string can
// never become shell syntax. Targets reach here validated, but building a
// command string out of them would be a needless injection surface.
//
// `;` rather than `&&`, also deliberately: if the tool fails outright no report
// file exists, cat exits non-zero with empty stdout, and runContainerCLI
// surfaces that as a scan error instead of an empty (i.e. clean-looking) report.
func netscannerCollect(tool, reportPath string, args []string) []string {
	script := tool + ` "$@" >/dev/null 2>&1; cat ` + reportPath
	return append([]string{"-c", script, "sh"}, args...)
}
