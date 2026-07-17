#!/bin/sh
# Populates/refreshes the scanner vulnerability databases in the cache volume
# mounted at $AEGIS_CACHE_DIR (default /cache).
#
# This is the ONLY part of the multiscanner that is meant to touch the network.
# `aegis security update-db` runs it with networking enabled; actual scans run
# the same image with --network none and read what this left behind. That split
# is the whole point: the image stays small (no baked databases) without giving
# every scan run outbound network access while the workspace is mounted.
#
# Run by hand for debugging:
#   podman run --rm --network bridge -v aegis-scanner-cache:/cache \
#     localhost/aegis-multiscanner:v1 aegis-update-db
set -eu

CACHE="${AEGIS_CACHE_DIR:-/cache}"
OSV_DB_BASE="${OSV_DB_BASE:-https://osv-vulnerabilities.storage.googleapis.com}"
OSV_ECOSYSTEMS="${OSV_ECOSYSTEMS:-Go npm PyPI Maven RubyGems crates.io NuGet Packagist Hex Pub}"

mkdir -p "$CACHE"

echo "==> trivy vulnerability DB"
# The image sets TRIVY_SKIP_*_UPDATE=true so scans never try to fetch; this is
# the one context where updating is the job, so they're flipped off here.
TRIVY_SKIP_DB_UPDATE=false trivy image --download-db-only

if [ "${AEGIS_SKIP_JAVA_DB:-false}" = "true" ]; then
	echo "==> trivy Java DB: skipped (AEGIS_SKIP_JAVA_DB=true)"
else
	echo "==> trivy Java DB (~1.4GB; set AEGIS_SKIP_JAVA_DB=true to skip if you never scan JVM code)"
	TRIVY_SKIP_JAVA_DB_UPDATE=false trivy image --download-java-db-only
fi

echo "==> osv-scanner offline databases"
# Fetched directly rather than via `osv-scanner --download-offline-databases`,
# which only pulls the ecosystems a probe project happens to reference and
# otherwise leaves an empty cache. An empty cache is the dangerous case:
# osv-scanner then reports zero findings instead of failing, so every ecosystem
# is fetched explicitly and each archive is verified.
osv_root="${XDG_CACHE_HOME:-$HOME/.cache}/osv-scanner"
for eco in $OSV_ECOSYSTEMS; do
	dir="${osv_root}/${eco}"
	mkdir -p "$dir"
	printf '    %s ... ' "$eco"
	curl -fsSL --retry 3 "${OSV_DB_BASE}/${eco}/all.zip" -o "${dir}/all.zip"
	if ! unzip -tq "${dir}/all.zip" >/dev/null 2>&1; then
		echo "FAILED (not a valid zip)"
		echo "${eco}/all.zip is not a valid archive — refusing to leave an unusable DB in place" >&2
		rm -f "${dir}/all.zip"
		exit 1
	fi
	echo "ok"
done

# An empty-but-valid cache would still scan clean and report nothing, so assert
# the largest ecosystem is actually substantial.
if [ "$(stat -c%s "${osv_root}/npm/all.zip")" -lt 1000000 ]; then
	echo "npm OSV database is implausibly small — refusing to treat this as a successful update" >&2
	exit 1
fi

echo "==> done"
du -sh "$CACHE" 2>/dev/null || true
