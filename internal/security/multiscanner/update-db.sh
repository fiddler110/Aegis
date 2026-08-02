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
# Each step below is INDEPENDENT (P55.2). The script used to be a straight-line
# `set -eu` sequence, so the first failing fetch aborted every later one and
# left a half-populated cache with no summary — the observed state on one
# machine was trivy and osv-scanner fully populated while grype had no database
# at all, from a single early `grype db update` failure, reported as one error
# and a non-zero exit. Nothing said "2 of 3 are fine". Now every step runs
# whatever its predecessors did, outcomes are collected, and a summary is
# printed at the end; the exit status is non-zero if ANY step failed.
#
# Run by hand for debugging:
#   podman run --rm --network bridge -v aegis-scanner-cache:/cache \
#     localhost/aegis-multiscanner:v1 aegis-update-db
set -eu

CACHE="${AEGIS_CACHE_DIR:-/cache}"
OSV_DB_BASE="${OSV_DB_BASE:-https://osv-vulnerabilities.storage.googleapis.com}"
OSV_ECOSYSTEMS="${OSV_ECOSYSTEMS:-Go npm PyPI Maven RubyGems crates.io NuGet Packagist Hex Pub}"

# ── step helpers ─────────────────────────────────────────────────────────────
#
# fail records a one-line human reason for the summary and aborts the step.
# Steps run as child processes (see the dispatcher at the bottom), so exiting
# here ends only the step, not the run.
#
# Exit 3 is the "skipped" sentinel — a deliberate opt-out (AEGIS_SKIP_JAVA_DB)
# is neither success nor failure and must not colour the exit status.
STEP_SKIPPED=3

record_reason() {
	if [ -n "${AEGIS_STEP_REASON:-}" ]; then
		printf '%s\n' "$*" >"$AEGIS_STEP_REASON"
	fi
}

fail() {
	printf '%s\n' "$*" >&2
	record_reason "$*"
	exit 1
}

skip() {
	printf '%s\n' "$*"
	record_reason "$*"
	exit "$STEP_SKIPPED"
}

# file_size prints the size in bytes, or 0 when the file is missing, so the
# plausibility assertions below can be written as a single arithmetic test
# instead of an existence test plus a size test.
file_size() {
	[ -f "$1" ] || { echo 0; return 0; }
	stat -c%s "$1" 2>/dev/null || echo 0
}

# ── steps ────────────────────────────────────────────────────────────────────
#
# Each step is a function invoked in its own child shell. Nothing below may
# assume an earlier step succeeded.

step_trivy_db() {
	echo "==> trivy vulnerability DB"
	# The image sets TRIVY_SKIP_*_UPDATE=true so scans never try to fetch; this
	# is the one context where updating is the job, so they're flipped off here.
	TRIVY_SKIP_DB_UPDATE=false trivy image --download-db-only \
		|| fail "trivy --download-db-only failed"
}

step_trivy_java_db() {
	if [ "${AEGIS_SKIP_JAVA_DB:-false}" = "true" ]; then
		skip "AEGIS_SKIP_JAVA_DB=true (JVM code will not be scanned for CVEs)"
	fi
	echo "==> trivy Java DB (~1.4GB; set AEGIS_SKIP_JAVA_DB=true to skip if you never scan JVM code)"
	TRIVY_SKIP_JAVA_DB_UPDATE=false trivy image --download-java-db-only \
		|| fail "trivy --download-java-db-only failed"
}

step_trivy_checks() {
	echo "==> trivy misconfiguration checks bundle"
	# Without this the cache has no policy bundle, and EVERY scan logs
	#   ERROR [misconfig] failed to check cache: cache does not exist at
	#   "/cache/trivy/policy/content"
	# then silently falls back to the checks embedded in the trivy binary. That
	# still produces findings, so it's degraded rather than broken — but it is
	# an ERROR on every single scan (which trains operators to ignore scanner
	# errors), and the embedded set is frozen at the pinned trivy's build date
	# rather than the current bundle.
	#
	# There is deliberately no `--download-checks-only` flag at trivy 0.72:
	# upstream ships a download-only path for the vulnerability and Java DBs
	# only, and documents the checks bundle as being fetched (as an OCI
	# artifact) on the first misconfiguration scan and cached under
	# $TRIVY_CACHE_DIR/policy. So the supported way to pre-populate it is to
	# perform one misconfiguration scan with the check update enabled. This
	# runs the same `trivy fs --scanners misconfig` entry point a real scan
	# uses (see trivyScanArgs in ../scanners.go), which is what makes the cache
	# it writes the cache a real scan reads back.
	#
	# The target is a throwaway directory holding one trivial Dockerfile rather
	# than /src: update-db mounts no workspace, and an entirely empty directory
	# gives trivy no reason to engage the misconfig scanner at all.
	probe=$(mktemp -d)
	# shellcheck disable=SC2064 # expand $probe now, at trap-set time
	trap "rm -rf '$probe'" EXIT
	printf 'FROM scratch\nUSER root\n' >"${probe}/Dockerfile"
	TRIVY_SKIP_CHECK_UPDATE=false trivy fs --scanners misconfig --format json \
		--quiet "$probe" >/dev/null \
		|| fail "trivy misconfig probe scan failed (checks bundle not fetched)"

	# Assert the bundle actually landed. A trivy that failed to reach the
	# registry logs the fetch failure and falls back to embedded checks with
	# exit 0 — the same "clean exit, nothing loaded" shape the assertions below
	# exist for — so the scan succeeding proves nothing on its own.
	content="${TRIVY_CACHE_DIR:-${CACHE}/trivy}/policy/content"
	if [ ! -d "$content" ] || [ -z "$(ls -A "$content" 2>/dev/null)" ]; then
		fail "trivy checks bundle is not in the cache at ${content} — trivy fell back to its embedded checks"
	fi
}

step_grype_db() {
	echo "==> grype vulnerability DB"
	# The image sets GRYPE_DB_AUTO_UPDATE=false so scans never fetch; this run
	# is the exception, so it's flipped on. `grype db update` hydrates
	# $GRYPE_DB_CACHE_DIR/<schema>/vulnerability.db (the file
	# verifyMultiscannerCache checks for). VALIDATE_AGE stays off so the
	# download isn't rejected as stale.
	GRYPE_DB_AUTO_UPDATE=true grype db update || fail "grype db update failed"

	# An empty/partial DB would still scan clean and report nothing (grype
	# doesn't fail on a thin DB), so assert the vulnerability.db grype just
	# wrote is substantial. Globbed rather than hardcoding the schema
	# subdirectory (6 today).
	grype_db=$(ls -1 "${GRYPE_DB_CACHE_DIR:-${CACHE}/grype/db}"/*/vulnerability.db 2>/dev/null | head -n1 || true)
	if [ "$(file_size "$grype_db")" -lt 10000000 ]; then
		fail "grype vulnerability DB is missing or implausibly small — refusing to treat this as a successful update"
	fi
}

step_osv_db() {
	echo "==> osv-scanner offline databases"
	# Fetched directly rather than via `osv-scanner
	# --download-offline-databases`, which only pulls the ecosystems a probe
	# project happens to reference and otherwise leaves an empty cache. An
	# empty cache is the dangerous case: osv-scanner then reports zero findings
	# instead of failing, so every ecosystem is fetched explicitly and each
	# archive is verified.
	#
	# A single ecosystem's failure no longer abandons the rest — the loop
	# records it and carries on, and the step fails at the end naming every
	# ecosystem that did not land. Same reasoning as the per-step split: a
	# partial result the operator can see beats an opaque early abort.
	osv_root="${XDG_CACHE_HOME:-$HOME/.cache}/osv-scanner"
	osv_failed=""
	for eco in $OSV_ECOSYSTEMS; do
		dir="${osv_root}/${eco}"
		mkdir -p "$dir"
		printf '    %s ... ' "$eco"
		if ! curl -fsSL --retry 3 "${OSV_DB_BASE}/${eco}/all.zip" -o "${dir}/all.zip"; then
			echo "FAILED (download)"
			rm -f "${dir}/all.zip"
			osv_failed="${osv_failed} ${eco}"
			continue
		fi
		if ! unzip -tq "${dir}/all.zip" >/dev/null 2>&1; then
			echo "FAILED (not a valid zip)"
			# Removed rather than left behind: an unusable archive in place is
			# worse than none, since osv-scanner would read it as an empty DB.
			rm -f "${dir}/all.zip"
			osv_failed="${osv_failed} ${eco}"
			continue
		fi
		echo "ok"
	done

	# An empty-but-valid cache would still scan clean and report nothing, so
	# assert the largest ecosystem is actually substantial.
	if [ "$(file_size "${osv_root}/npm/all.zip")" -lt 1000000 ]; then
		fail "npm OSV database is missing or implausibly small — refusing to treat this as a successful update"
	fi
	if [ -n "$osv_failed" ]; then
		fail "ecosystems failed:${osv_failed}"
	fi
}

# ── dispatcher ───────────────────────────────────────────────────────────────
#
# Steps run as child processes (`sh "$0" --run-step <name>`) rather than as
# plain function calls. That is what makes independence and `set -e` coexist:
# a function or subshell evaluated as an `if` condition has errexit suppressed
# for everything inside it, so a mid-step command failure would sail past
# unnoticed — exactly the intra-step error detection this script needs. A
# separate process keeps its own `set -eu` fully armed while the parent merely
# reads its exit status.

if [ "${1:-}" = "--run-step" ]; then
	case "${2:-}" in
	trivy-db) step_trivy_db ;;
	trivy-java-db) step_trivy_java_db ;;
	trivy-checks) step_trivy_checks ;;
	grype-db) step_grype_db ;;
	osv-db) step_osv_db ;;
	*)
		echo "unknown step: ${2:-}" >&2
		exit 64
		;;
	esac
	exit 0
fi

mkdir -p "$CACHE"

state=$(mktemp -d)
trap 'rm -rf "$state"' EXIT

failed=0
results=""

for step in trivy-db trivy-java-db trivy-checks grype-db osv-db; do
	reason_file="${state}/${step}.reason"
	: >"$reason_file"
	status=0
	AEGIS_STEP_REASON="$reason_file" sh "$0" --run-step "$step" || status=$?
	reason=$(head -n1 "$reason_file" 2>/dev/null || true)
	case "$status" in
	0) outcome="ok" ;;
	"$STEP_SKIPPED") outcome="skipped${reason:+ — $reason}" ;;
	*)
		# Every assertion above writes its own reason; the generic exit-status
		# form is the fallback for a tool that died without one of ours.
		outcome="FAILED: ${reason:-exit status ${status}}"
		failed=1
		;;
	esac
	results="${results}${step}|${outcome}
"
done

echo
echo "==> summary"
printf '%s' "$results" | while IFS='|' read -r name outcome; do
	[ -n "$name" ] || continue
	printf '    %-14s %s\n' "$name" "$outcome"
done

du -sh "$CACHE" 2>/dev/null || true

if [ "$failed" -ne 0 ]; then
	echo
	echo "==> one or more databases were not updated; the cache is PARTIALLY populated." >&2
	echo "    Scanners whose database is missing will be refused by container-method scans" >&2
	echo "    (see verifyMultiscannerCache); the ones marked ok above are usable now." >&2
	echo "    Re-run \`aegis security update-db\` to retry only what failed — steps that" >&2
	echo "    already succeeded are cheap no-ops." >&2
	exit 1
fi

echo "==> done"
