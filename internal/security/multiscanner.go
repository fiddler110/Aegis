package security

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// The multiscanner is a single locally-built image carrying every scanner
// Aegis drives against a directory, so an operator provisions once rather
// than installing 16 tools or pinning 16 separate images.
//
// It exists because ScannerDescriptor.DefaultImage is deliberately empty for
// every tool — Aegis never ships an unverified default image — which left the
// container path resolving to MethodNone out of the box. A locally-built
// image sidesteps that: the operator builds it from a Containerfile in this
// repo, so there's no third-party registry artifact to trust in the first
// place.
const (
	// MultiscannerProfileCore carries only the statically-linked scanners.
	MultiscannerProfileCore = "core"
	// MultiscannerProfileFull adds the Python (semgrep/bandit/njsscan), Ruby
	// (brakeman), and network (nmap/nuclei) scanners.
	MultiscannerProfileFull = "full"

	// MultiscannerDefaultImage is the tag `aegis security build-image`
	// applies. "localhost/" is not decoration: it marks the reference as
	// local-only so a runtime never treats a missing image as a registry pull.
	MultiscannerDefaultImage = "localhost/aegis-multiscanner:v1"

	// MultiscannerCacheVolume holds the scanner vulnerability databases.
	//
	// They live here rather than baked into the image because they were ~3.7GB
	// of a 5.8GB image. A volume is not merely nicer — it's required: scanner
	// containers run with --rm, so without a persistent cache every scan would
	// re-download trivy's ~1.2GB database from scratch.
	//
	// It is populated by `aegis security update-db`, the only container run
	// Aegis gives network access. Scans mount the same volume and still run
	// --network none, so the workspace is never mounted into a networked
	// container.
	MultiscannerCacheVolume = "aegis-scanner-cache"
	// multiscannerCacheMount is where MultiscannerCacheVolume is mounted; it
	// must match the cache paths the Containerfile sets (TRIVY_CACHE_DIR,
	// XDG_CACHE_HOME).
	multiscannerCacheMount = "/cache"

	// multiscannerDefaultConcurrency bounds how many scanners run at once when
	// security.multiscanner.concurrency isn't set. Each container-method
	// scanner is a container, and each is CPU/IO-hungry, so this stays modest.
	multiscannerDefaultConcurrency = 3

	// multiscannerVerifyTTL bounds how long one image-ID verification is
	// reused. It exists because Options is built once per loaded config but
	// held for the daemon's lifetime (securityScanTool keeps it at
	// construction) — a verify-once memo would happily vouch for an image the
	// operator rebuilt an hour ago. A short TTL collapses the ~16 Resolve
	// calls within a single scan run into one inspect, while still noticing a
	// rebuild before the next run.
	multiscannerVerifyTTL = 15 * time.Second
)

// Scanner containers run with --network none, so any tool that would normally
// fetch something on first use needs that data baked into the image and needs
// to be told to use the local copy. These are the paths and flags for that,
// and they must stay in step with the Containerfile.
//
// This is not a theoretical concern: opengrep's "pinned" rule packs
// (p/owasp-top-ten) are fetched from semgrep.dev at scan time, and osv-scanner
// queries api.osv.dev per run. Both fail outright against a network-less
// container until pointed at baked copies.
const (
	// multiscannerRulesDir holds the SAST rule packs downloaded at build time.
	multiscannerRulesDir = "/opt/semgrep-rules"
	// multiscannerOfflineFlag makes osv-scanner match against the offline
	// database baked into the image instead of calling api.osv.dev.
	multiscannerOfflineFlag = "--offline-vulnerabilities"
)

// multiscannerDBTools maps each scanner that needs a database from the cache
// volume to a file that must exist there before it can produce a trustworthy
// result.
//
// This check exists because of how these two fail on an empty cache, which is
// not the same:
//
//   - trivy exits non-zero with empty stdout ("--skip-db-update cannot be
//     specified on the first run"), so it surfaces as a scan error already.
//   - osv-scanner logs "no offline version of the OSV database is available"
//     to stderr but still writes a valid, empty JSON report to stdout. Since
//     runContainerCLI only treats a non-zero exit as failure when stdout is
//     empty (scanners routinely exit non-zero when they find things), that
//     becomes "osv-scanner: 0 findings" — a silent all-clear from a scanner
//     that never looked at a database.
//
// So the cache is verified up front rather than trusting each tool to complain.
var multiscannerDBTools = map[string]string{
	"trivy":       "/cache/trivy/db/trivy.db",
	"osv-scanner": "/cache/osv-scanner/npm/all.zip",
}

// verifyMultiscannerCache reports a MethodNone reason when name needs a
// database that hasn't been downloaded into the cache volume yet.
func verifyMultiscannerCache(ctx context.Context, rt sandbox.ContainerRuntime, name string, p MultiscannerPolicy) string {
	marker, needsDB := multiscannerDBTools[name]
	if !needsDB {
		return ""
	}
	key := "cache|" + string(rt) + "|" + p.Image + "|" + name
	if c := p.cacheCheck; c != nil {
		c.mu.Lock()
		if c.key == key && time.Since(c.at) < multiscannerVerifyTTL {
			fail := c.fail
			c.mu.Unlock()
			return fail
		}
		c.mu.Unlock()
	}

	fail := ""
	if !cacheFileExists(ctx, rt, p.Image, marker) {
		fail = name + " needs a vulnerability database, and the " + MultiscannerCacheVolume +
			" volume doesn't have one yet — run `aegis security update-db` (it is the only step that needs network access)"
	}

	if c := p.cacheCheck; c != nil {
		c.mu.Lock()
		c.key, c.at, c.fail = key, time.Now(), fail
		c.mu.Unlock()
	}
	return fail
}

// cacheFileExists is a seam over the containerized cache probe so tests can
// exercise resolution without a runtime.
var cacheFileExists = func(ctx context.Context, rt sandbox.ContainerRuntime, image, path string) bool {
	cmd := exec.CommandContext(ctx, string(rt), "run", "--rm", "--network", "none",
		"-v", MultiscannerCacheVolume+":"+multiscannerCacheMount,
		"--entrypoint=", image, "test", "-e", path)
	return cmd.Run() == nil
}

// multiscannerRulePackPath maps a registry pack reference ("p/owasp-top-ten")
// to the baked copy the Containerfile writes.
func multiscannerRulePackPath(pack string) string {
	return multiscannerRulesDir + "/" + strings.TrimPrefix(pack, "p/") + ".yaml"
}

// sastScanArgsFor is sastScanArgs with the registry pack references swapped
// for baked local paths when running out of the multiscanner image. Any other
// image keeps the registry references, since only this image is known to carry
// the packs.
func sastScanArgsFor(opts Options, image string) []string {
	if !opts.usesMultiscanner(image) {
		return sastScanArgs()
	}
	args := []string{"--sarif", "--quiet"}
	for _, pack := range sastRulePacks {
		args = append(args, "--config", multiscannerRulePackPath(pack))
	}
	return args
}

// multiscannerCoreTools are the scanners present in every profile. Ordered as
// the Containerfile installs them.
var multiscannerCoreTools = []string{
	"trivy", "gitleaks", "trufflehog", "syft",
	"osv-scanner", "kubescape", "hadolint", "opengrep",
}

// multiscannerFullOnlyTools need an interpreter or a network stack, so they
// only exist in the full profile.
var multiscannerFullOnlyTools = []string{
	"semgrep", "bandit", "brakeman", "njsscan", "nmap", "nuclei",
}

// multiscannerExcludedTools are scanners the shared image deliberately does
// not carry, and why. Kept as data rather than prose so the resolver can tell
// an operator the actual reason instead of "rebuild with --profile full" for a
// tool no profile will ever include.
var multiscannerExcludedTools = map[string]string{
	"zap":    "zap is a large Java app with its own mount contract (/zap/wrk) and automation-framework invocation; it keeps its own official image",
	"dockle": "dockle inspects an image through the local container engine, so it needs the engine socket and can't run from inside a scanner container",
	// grype is excluded by decision rather than by a technical constraint: it
	// works fine in the image, but trivy and osv-scanner were kept as the SCA
	// coverage and grype's database was the single largest cache item (~1.8GB).
	// Worth knowing if you revisit this: on the Aegis repo grype reported 47
	// findings where trivy reported 3 and osv-scanner 1, so this trades real
	// dependency-CVE coverage for a smaller cache. It stays a registered
	// scanner, so `method: host` still runs a host-installed grype.
	"grype": "grype is not carried by the multiscanner image by decision (its DB was the largest single item in the cache) — install it on the host (`aegis security install grype`) and it will run via method: host, or set security.tools.grype.image to give it its own image",
	// gosec is the subtle one, and the reason it's excluded is worth stating
	// precisely: it is a compile-assisted analyzer. It resolves packages via
	// `go list`, which needs a Go toolchain and the module cache — i.e. the
	// network — and scanner containers run with --network none. It does not
	// fail when it can't do that; it reports zero findings and exits clean.
	// Measured on this repo: the host finds 244 issues, the container found 0.
	// A silent all-clear is the worst possible failure for a security tool, so
	// gosec runs on the host or not at all.
	"gosec": "gosec needs a Go toolchain and module resolution (network) to load packages, which a --network none scanner container can't provide — and it reports zero findings rather than failing when it can't, so it runs on the host instead (`aegis security install gosec`)",
}

// MultiscannerTools returns the scanners the named profile's image carries,
// sorted. An unknown profile returns the full set — `aegis security
// build-image` validates the profile up front, so this only backstops a
// hand-edited config, where over-reporting is caught by the resolver anyway
// (a tool the image lacks fails its own scan, loudly, not silently).
func MultiscannerTools(profile string) []string {
	out := append([]string(nil), multiscannerCoreTools...)
	if strings.TrimSpace(strings.ToLower(profile)) != MultiscannerProfileCore {
		out = append(out, multiscannerFullOnlyTools...)
	}
	sort.Strings(out)
	return out
}

// MultiscannerProfiles lists the valid --profile values.
func MultiscannerProfiles() []string {
	return []string{MultiscannerProfileCore, MultiscannerProfileFull}
}

// MultiscannerPolicy is the resolver's view of the shared image, translated
// from config.MultiscannerConfig by MultiscannerPolicyFromConfig. Decoupled
// from internal/config the same way ToolPolicy is.
type MultiscannerPolicy struct {
	Enabled     bool
	Image       string
	ImageID     string
	Concurrency int
	// Runtime is the container runtime that built the image. See
	// RuntimePriority for why resolution can't just auto-detect.
	Runtime sandbox.ContainerRuntime
	// Tools is the set of scanners this image carries. Populated by `aegis
	// security build-image` from the profile it actually built.
	Tools map[string]bool

	// check memoizes the image-ID verification for multiscannerVerifyTTL. A
	// pointer so it survives Options being copied by value through Resolve.
	// Nil is valid and simply means "verify on every call" — the shape a test
	// constructing Options directly gets.
	check *multiscannerCheck
	// cacheCheck memoizes the database-cache probe, which costs a container
	// run. Separate from check so an image-ID hit doesn't mask a cache miss.
	cacheCheck *multiscannerCheck
}

// Covers reports whether name should resolve to the shared image.
func (m MultiscannerPolicy) Covers(name string) bool {
	return m.Enabled && m.Image != "" && m.Tools[name]
}

// RuntimePriority is the runtime probe order to use when resolving the shared
// image: the one that built it, if recorded, and nothing else.
//
// A locally-built image is not portable between runtimes — it lives in the
// storage of whichever engine built it, and is never pulled. Auto-detecting
// instead would silently look in the wrong place: on Windows, DetectBest
// prefers wslc, so a podman-built image gets reported as missing while sitting
// right there in podman's storage. Returning nil (no recorded runtime) keeps
// the pre-multiscanner auto-detect behavior.
func (m MultiscannerPolicy) RuntimePriority() []sandbox.ContainerRuntime {
	if m.Runtime == "" {
		return nil
	}
	return []sandbox.ContainerRuntime{m.Runtime}
}

// EffectiveConcurrency is how many scanners may run at once.
func (m MultiscannerPolicy) EffectiveConcurrency() int {
	if m.Concurrency > 0 {
		return m.Concurrency
	}
	return multiscannerDefaultConcurrency
}

// scanConcurrency is how many scanners RunWithProgress runs at once. It reads
// from the multiscanner block because that's where an operator tuning "how
// many containers at once" would look, but it applies to host-method runs
// equally — those are subprocesses with the same reason to overlap.
func (o Options) scanConcurrency() int {
	if n := o.Multiscanner.EffectiveConcurrency(); n > 0 {
		return n
	}
	return 1
}

// MultiscannerPolicyFromConfig translates the user-facing config block.
func MultiscannerPolicyFromConfig(cfg config.MultiscannerConfig) MultiscannerPolicy {
	p := MultiscannerPolicy{
		Enabled:     cfg.Enabled,
		Image:       strings.TrimSpace(cfg.Image),
		ImageID:     strings.TrimSpace(cfg.ImageID),
		Runtime:     sandbox.ContainerRuntime(strings.TrimSpace(cfg.Runtime)),
		Concurrency: cfg.Concurrency,
		Tools:       map[string]bool{},
		check:       &multiscannerCheck{},
		cacheCheck:  &multiscannerCheck{},
	}
	names := cfg.Tools
	if len(names) == 0 {
		// An enabled block with no explicit tool list predates or bypasses
		// build-image (which always writes one). Assume the full profile: a
		// tool the image turns out to lack fails its own scan with the
		// container's own error, which is a visible, specific failure — the
		// opposite of the silent skip P11.1 rules out.
		names = MultiscannerTools(MultiscannerProfileFull)
	}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			p.Tools[n] = true
		}
	}
	return p
}

// multiscannerCheck caches one image-ID verification result under a TTL.
type multiscannerCheck struct {
	mu   sync.Mutex
	at   time.Time
	key  string
	fail string
}

// inspectImageID is a seam over the runtime's image inspect so tests can
// verify resolution without a real docker/podman install, mirroring how
// detectRuntime seams sandbox.DetectBest.
var inspectImageID = func(ctx context.Context, rt sandbox.ContainerRuntime, image string) (string, error) {
	cmd := exec.CommandContext(ctx, string(rt), "image", "inspect", "--format", "{{.Id}}", image)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s image inspect %s: %w: %s", rt, image, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s image inspect %s: %w", rt, image, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// normalizeImageID strips the "sha256:" prefix and lowercases, so the two
// runtimes' formats compare equal: docker's inspect returns
// "sha256:<hex>", podman's returns bare "<hex>".
func normalizeImageID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.TrimPrefix(id, "sha256:")
}

// verifyMultiscannerImage confirms the image currently answering to
// policy.Image is byte-for-byte the one build-image recorded, returning a
// MethodNone reason if not.
//
// This is what stands in for digestPinReason on the multiscanner path, and it
// is a stronger check rather than a waived one. A locally-built image has no
// registry digest to pin — RepoDigests is empty until a push or pull — so an
// "image@sha256:..." reference cannot exist for one and the regex could only
// ever be satisfied by a fiction. Asking the runtime for the image's real ID
// and comparing it verifies the actual content, which is what the digest rule
// was reaching for in the first place.
func verifyMultiscannerImage(ctx context.Context, rt sandbox.ContainerRuntime, p MultiscannerPolicy) string {
	if p.ImageID == "" {
		return "security.multiscanner.enabled is true but no image has been built yet (security.multiscanner.image_id is empty) — run `aegis security build-image`"
	}
	key := string(rt) + "|" + p.Image + "|" + p.ImageID
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
		// Deliberately "could not be verified" rather than "is missing": the
		// inspect can fail for reasons other than an absent image (engine not
		// running, a runtime whose CLI doesn't take these flags), and claiming
		// the image is gone would send the reader chasing the wrong problem.
		fail = fmt.Sprintf("multiscanner image %s could not be verified in %s's local image storage (%v) — if it was removed, re-run `aegis security build-image`", p.Image, rt, err)
	case normalizeImageID(actual) != normalizeImageID(p.ImageID):
		fail = fmt.Sprintf("multiscanner image %s no longer matches the ID recorded in config (have %s, expected %s) — it was rebuilt or retagged; re-run `aegis security build-image` to re-pin it", p.Image, shortImageID(actual), shortImageID(p.ImageID))
	}

	if c := p.check; c != nil {
		c.mu.Lock()
		c.key, c.at, c.fail = key, time.Now(), fail
		c.mu.Unlock()
	}
	return fail
}

// shortImageID trims an image ID to the conventional 12 hex characters for
// error messages, where the full 64 would bury the point.
func shortImageID(id string) string {
	id = normalizeImageID(id)
	if len(id) > 12 {
		return id[:12]
	}
	if id == "" {
		return "<none>"
	}
	return id
}
