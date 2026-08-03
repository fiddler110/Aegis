#!/bin/sh
# Downloads every statically-linked scanner binary into /out, verifying each
# one against the checksum its project publishes alongside the release.
#
# On what that verification is and isn't: the checksum file is served by the
# same GitHub release as the artifact, so a compromised release could carry a
# matching checksum. It catches truncation, corruption, and a mismatched
# version/asset name, not a malicious upstream. The control that actually
# binds Aegis to *this* image is the image-ID pin recorded by `aegis security
# build-image` and re-verified before every container run.
#
# Every version is pinned by the caller (Containerfile ARGs). Nothing here
# resolves "latest" — a floating scanner version is the same class of problem
# as a floating image tag (P7.6).
set -eu

: "${TRIVY_VERSION:?}" "${GITLEAKS_VERSION:?}" "${TRUFFLEHOG_VERSION:?}"
: "${SYFT_VERSION:?}" "${OSV_SCANNER_VERSION:?}" "${GRYPE_VERSION:?}"
: "${KUBESCAPE_VERSION:?}" "${HADOLINT_VERSION:?}"
: "${OPENGREP_VERSION:?}" "${OPENGREP_SHA256:?}" "${NUCLEI_VERSION:?}"
: "${GOSEC_VERSION:?}" "${GO_VERSION:?}" "${GO_SHA256:?}"

OUT=/out
WORK=/tmp/fetch
mkdir -p "$OUT" "$WORK"

dl() { curl -fsSL --retry 3 --retry-delay 2 -O "$1"; }

# verify <checksums-file> <artifact>
#
# Pulls out only the line naming <artifact> and feeds that to sha256sum.
# Deliberately not `sha256sum --ignore-missing -c <file>`: --ignore-missing
# skips entries whose file is absent, so a typo'd or renamed artifact would
# verify *nothing* and still exit 0. Here, a missing entry means grep emits
# nothing, sha256sum finds no properly-formatted line, and the build fails.
# Fails closed.
#
# The [ *] class matches both checksum dialects in use: "<hash>  name"
# (trivy, syft, osv-scanner, ...) and "<hash> *name" (hadolint's binary-mode
# marker).
verify() {
	if ! grep -E "[ *]${2}\$" "$1" | sha256sum -c -; then
		echo "checksum verification failed for $2 (no entry in $1, or hash mismatch)" >&2
		exit 1
	fi
}

# Each fetcher runs in its own subshell + scratch dir so a stray extracted
# file can never be picked up by the next one.
fetch() {
	name=$1
	shift
	rm -rf "${WORK:?}/$name"
	mkdir -p "$WORK/$name"
	(cd "$WORK/$name" && "$@")
	echo "  fetched $name"
}

get_trivy() {
	base="https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}"
	dl "${base}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz"
	dl "${base}/trivy_${TRIVY_VERSION}_checksums.txt"
	verify "trivy_${TRIVY_VERSION}_checksums.txt" "trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz"
	tar -xzf "trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" trivy
	install -m 0755 trivy "$OUT/trivy"
}

get_gitleaks() {
	base="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}"
	dl "${base}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
	dl "${base}/gitleaks_${GITLEAKS_VERSION}_checksums.txt"
	verify "gitleaks_${GITLEAKS_VERSION}_checksums.txt" "gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
	tar -xzf "gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" gitleaks
	install -m 0755 gitleaks "$OUT/gitleaks"
}

get_trufflehog() {
	base="https://github.com/trufflesecurity/trufflehog/releases/download/v${TRUFFLEHOG_VERSION}"
	dl "${base}/trufflehog_${TRUFFLEHOG_VERSION}_linux_amd64.tar.gz"
	dl "${base}/trufflehog_${TRUFFLEHOG_VERSION}_checksums.txt"
	verify "trufflehog_${TRUFFLEHOG_VERSION}_checksums.txt" "trufflehog_${TRUFFLEHOG_VERSION}_linux_amd64.tar.gz"
	tar -xzf "trufflehog_${TRUFFLEHOG_VERSION}_linux_amd64.tar.gz" trufflehog
	install -m 0755 trufflehog "$OUT/trufflehog"
}

get_syft() {
	base="https://github.com/anchore/syft/releases/download/v${SYFT_VERSION}"
	dl "${base}/syft_${SYFT_VERSION}_linux_amd64.tar.gz"
	dl "${base}/syft_${SYFT_VERSION}_checksums.txt"
	verify "syft_${SYFT_VERSION}_checksums.txt" "syft_${SYFT_VERSION}_linux_amd64.tar.gz"
	tar -xzf "syft_${SYFT_VERSION}_linux_amd64.tar.gz" syft
	install -m 0755 syft "$OUT/syft"
}

get_osv_scanner() {
	base="https://github.com/google/osv-scanner/releases/download/v${OSV_SCANNER_VERSION}"
	dl "${base}/osv-scanner_linux_amd64"
	dl "${base}/osv-scanner_SHA256SUMS"
	verify "osv-scanner_SHA256SUMS" "osv-scanner_linux_amd64"
	install -m 0755 osv-scanner_linux_amd64 "$OUT/osv-scanner"
}

get_grype() {
	base="https://github.com/anchore/grype/releases/download/v${GRYPE_VERSION}"
	dl "${base}/grype_${GRYPE_VERSION}_linux_amd64.tar.gz"
	dl "${base}/grype_${GRYPE_VERSION}_checksums.txt"
	verify "grype_${GRYPE_VERSION}_checksums.txt" "grype_${GRYPE_VERSION}_linux_amd64.tar.gz"
	tar -xzf "grype_${GRYPE_VERSION}_linux_amd64.tar.gz" grype
	install -m 0755 grype "$OUT/grype"
}

get_kubescape() {
	base="https://github.com/kubescape/kubescape/releases/download/v${KUBESCAPE_VERSION}"
	dl "${base}/kubescape_${KUBESCAPE_VERSION}_linux_amd64"
	dl "${base}/checksums.sha256"
	verify "checksums.sha256" "kubescape_${KUBESCAPE_VERSION}_linux_amd64"
	install -m 0755 "kubescape_${KUBESCAPE_VERSION}_linux_amd64" "$OUT/kubescape"
}

get_hadolint() {
	base="https://github.com/hadolint/hadolint/releases/download/v${HADOLINT_VERSION}"
	dl "${base}/hadolint-linux-x86_64"
	dl "${base}/hadolint-linux-x86_64.sha256"
	verify "hadolint-linux-x86_64.sha256" "hadolint-linux-x86_64"
	install -m 0755 hadolint-linux-x86_64 "$OUT/hadolint"
}

# opengrep is the one scanner with no published checksum file — its releases
# carry sigstore .cert/.sig pairs instead, which would need cosign plus a
# trusted-identity policy to check properly. Rather than skip verification,
# the expected digest is pinned directly as a build ARG (OPENGREP_SHA256),
# recorded from the release artifact. Bump it whenever OPENGREP_VERSION moves;
# a stale pair fails the build loudly rather than installing something
# unverified.
get_opengrep() {
	base="https://github.com/opengrep/opengrep/releases/download/v${OPENGREP_VERSION}"
	dl "${base}/opengrep_manylinux_x86"
	echo "${OPENGREP_SHA256}  opengrep_manylinux_x86" >opengrep.sha256
	verify "opengrep.sha256" "opengrep_manylinux_x86"
	install -m 0755 opengrep_manylinux_x86 "$OUT/opengrep"
}

# nuclei ships only in the full profile (network scanner), but is fetched here
# unconditionally to keep this stage cache-stable across profiles; the core
# final stage simply doesn't copy it.
get_nuclei() {
	base="https://github.com/projectdiscovery/nuclei/releases/download/v${NUCLEI_VERSION}"
	dl "${base}/nuclei_${NUCLEI_VERSION}_linux_amd64.zip"
	dl "${base}/nuclei_${NUCLEI_VERSION}_checksums.txt"
	verify "nuclei_${NUCLEI_VERSION}_checksums.txt" "nuclei_${NUCLEI_VERSION}_linux_amd64.zip"
	unzip -qo "nuclei_${NUCLEI_VERSION}_linux_amd64.zip" nuclei
	install -m 0755 nuclei "$OUT/nuclei"
}

# gosec, and the Go toolchain it cannot work without.
#
# gosec is compile-assisted: it resolves packages through `go list`, so without
# a toolchain it does not fail — it reports zero findings and exits clean
# (measured on the Aegis repo: host 244, toolchain-less container 0). Carrying
# the binary without carrying Go would therefore ship the exact silent
# all-clear this image's verification exists to rule out, which is why the two
# are fetched together and copied together (see the profile-full stage).
get_gosec() {
	base="https://github.com/securego/gosec/releases/download/v${GOSEC_VERSION}"
	dl "${base}/gosec_${GOSEC_VERSION}_linux_amd64.tar.gz"
	dl "${base}/gosec_${GOSEC_VERSION}_checksums.txt"
	verify "gosec_${GOSEC_VERSION}_checksums.txt" "gosec_${GOSEC_VERSION}_linux_amd64.tar.gz"
	tar -xzf "gosec_${GOSEC_VERSION}_linux_amd64.tar.gz" gosec
	install -m 0755 gosec "$OUT/gosec"
}

# The Go toolchain publishes its checksums on the download index (go.dev/dl,
# `?mode=json`) rather than as a file served beside the artifact, so — like
# opengrep — the expected digest is pinned directly as a build ARG (GO_SHA256)
# instead of being fetched from the same place as the tarball. Bump the two
# together; a stale pair fails the build loudly.
#
# Unpacked whole into /out/go rather than reduced to a binary: gosec needs the
# standard library and `go list`, not just the `go` command.
get_go() {
	dl "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
	echo "${GO_SHA256}  go${GO_VERSION}.linux-amd64.tar.gz" >go.sha256
	verify "go.sha256" "go${GO_VERSION}.linux-amd64.tar.gz"
	tar -xzf "go${GO_VERSION}.linux-amd64.tar.gz" go
	rm -rf "${OUT:?}/go"
	mv go "$OUT/go"
	# A toolchain that can't report its own version is not one gosec can drive,
	# and finding that out here beats finding it out as "gosec: 0 findings".
	"$OUT/go/bin/go" version
}

echo "fetching pinned scanner binaries..."
fetch trivy get_trivy
fetch gitleaks get_gitleaks
fetch trufflehog get_trufflehog
fetch syft get_syft
fetch osv-scanner get_osv_scanner
fetch grype get_grype
fetch kubescape get_kubescape
fetch hadolint get_hadolint
fetch opengrep get_opengrep
fetch nuclei get_nuclei
fetch gosec get_gosec
fetch go get_go

rm -rf "$WORK"
echo "all binaries verified and installed to $OUT"
ls -la "$OUT"
