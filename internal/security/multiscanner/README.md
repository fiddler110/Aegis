# Multiscanner image

One locally-built image carrying every scanner Aegis drives against a
directory, so an operator provisions once instead of installing 16 tools or
pinning 16 separate container images.

Build it with `aegis security build-image` — not `podman build` directly. The
command builds this Containerfile, reads back the resulting image ID, and
records it in config; that ID is what Aegis re-verifies before every container
run. A hand-built image works fine but nothing will use it until an ID is
pinned.

> **Editing this Containerfile? Rebuild the binary first.** It's `go:embed`-ed
> into `aegis`, so `build-image` uses the copy compiled into the binary, not
> the one on disk. Without `go build ./cmd/aegis` first, your edit is silently
> ignored and you'll debug the old image.

```
aegis security build-image                      # full profile, auto-detected runtime
aegis security build-image --profile core       # static binaries only, smaller
aegis security update-db                        # fill the DB cache volume (needs network)
```

## Why an image ID and not a digest

Every other scanner image in Aegis must be digest-pinned (`image@sha256:...`,
enforced by `digestPinReason` in `../method.go`). A locally-built image can't
satisfy that: `RepoDigests` stays empty until an image is pushed to or pulled
from a registry, so a `name@sha256:<id>` reference for one would be a fiction
that parses but doesn't resolve.

Rather than waive the rule, this path replaces it with a stronger check. The
image's real ID is read via `image inspect` and compared to the pinned one
before the first container run of a scan. A rebuilt or retagged image fails
closed with a specific reason. That verifies content, which is what the digest
rule was reaching for — a regex on a reference string never looked at the image
at all.

## Profiles

| | `core` | `full` (default) |
|---|---|---|
| Static binaries (trivy, gitleaks, trufflehog, syft, osv-scanner, kubescape, hadolint, opengrep) | ✅ | ✅ |
| Python: bandit, njsscan | | ✅ |
| Ruby: brakeman | | ✅ |
| Network: nmap, nuclei | | ✅ |
| Measured size | ~1.1GB | ~1.8GB |

## Databases live in a volume, not the image

Baked in, the vulnerability databases were ~3.7GB of a 5.8GB image. They now
live in the `aegis-scanner-cache` volume, mounted at `/cache`.

That's not only a size decision — it's what makes runtime fetching viable at
all. Scanner containers run with `--rm`, so without a persistent cache every
scan would re-download trivy's ~1.2GB database from scratch.

| | network | workspace mounted |
|---|---|---|
| `aegis security update-db` (runs `update-db.sh`) | **yes** | no |
| every scan | no (`--network none`) | yes |

Those two rows are the security argument: the one networked run has no source
to exfiltrate, and the runs with source mounted can't reach the network. If you
ever give scans network access, that property is gone.

The image still sets `TRIVY_SKIP_*_UPDATE=true` so a scan against an empty
cache fails with a clear "run update-db" rather than hanging on DNS.

## Not in the image, deliberately

Kept as data in `multiscannerExcludedTools` (`../multiscanner.go`) so the
resolver can explain itself rather than suggesting a rebuild that won't help.

- **zap** — a large Java app with a different mount contract (`/zap/wrk:rw`)
  and its own automation-framework invocation. Keeps its own official image;
  see `../dast.go`.
- **dockle** — inspects an image via the local container engine, so it needs
  the engine socket and can't run from inside a container the way the others
  do. See `../images.go`.
- **gosec** — a compile-assisted analyzer: it resolves packages via `go list`,
  which needs a Go toolchain and module downloads, and scanner containers run
  with `--network none`. Critically, it does **not** fail when it can't do
  that — it reports zero findings and exits clean. Measured on the Aegis repo:
  host 244 findings, container 0. Shipping it here would ship a silent
  all-clear, so it runs on the host or not at all.
- **grype** — excluded by decision, not by a technical constraint: it worked
  fine in the image, but trivy + osv-scanner were kept as the SCA coverage and
  grype's DB was the largest single cache item. If you revisit this: on the
  Aegis repo grype found 47 where trivy found 3 and osv-scanner 1, so it does
  trade real dependency-CVE coverage. It remains a registered scanner, so a
  host-installed grype still runs via `method: host`.
- **Image scanners** (`trivy image`, `grype <ref>`) — host-only by design;
  `runImageCmd` ignores the resolved method entirely.

## The rule this image keeps learning

Anything a scanner fetches at *first use* must come from the cache volume or be
baked in, **and** the tool must be *told* to use the local copy —
`--network none` is absolute for scans. That caught opengrep (its "pinned"
`p/owasp-top-ten` pack is still fetched from semgrep.dev at scan time),
osv-scanner (queries api.osv.dev per run, and needs
`--offline-vulnerabilities`), and gosec (above). Two of those failed loudly;
the third didn't.

When adding a scanner, assume it needs something from the network and prove
otherwise by running it in the built image against a real project. A unit test
cannot tell you this — every one of those three passed a fully green suite.

## Baked vulnerability databases

Scans run with `--network none` (see `containerRunArgs` in `../method.go`), so
anything a tool would normally fetch on first use has to already be present.
The build downloads the trivy and grype databases and sets
`TRIVY_SKIP_DB_UPDATE` / `GRYPE_DB_AUTO_UPDATE=false` so the tools use what's
baked in rather than failing against a network-less container. Nuclei templates
are fetched as a pinned release tarball for the same reason.

The trade is staleness: databases are only as fresh as the last build. Refresh
with `aegis security build-image --no-cache`.

## Pinning and verification

Every version is pinned by an `ARG` at the top of the Containerfile, and
`fetch.sh` verifies each download against the checksum its project publishes.
That catches truncation, corruption, and a wrong asset name — not a compromised
upstream release, since the checksum ships from the same place as the artifact.
The image-ID pin is the control that actually binds Aegis to a specific image.

`opengrep` is the one exception: it publishes sigstore `.cert`/`.sig` pairs
rather than a checksum file, so its digest is pinned directly as
`OPENGREP_SHA256`. Bump it together with `OPENGREP_VERSION` — a stale pair
fails the build loudly rather than installing something unverified.

To bump a scanner: change its `ARG`, rebuild, and re-pin. The build fails if
the release's asset names have changed.
