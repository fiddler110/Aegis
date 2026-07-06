# Recorded scanner output fixtures (P11.9)

These fixtures back `TestScanRegressionAcrossRecordedOutputs`
(`regression_test.go`): the P11.9 regression proof that a normalization/
dedup/ASVS/baseline change trips a test without needing any scanner binary,
container runtime, or network access in CI.

## Provenance

| File | Format | Representative of | Source |
|---|---|---|---|
| `semgrep_sast.sarif.json` | SARIF | semgrep/opengrep SAST | hand-authored, matches semgrep's documented `--sarif` output shape (rule `properties.tags`/`security-severity`) |
| `trivy_vuln.sarif.json` | SARIF | trivy `fs` dependency CVE | hand-authored, matches trivy's `--format sarif` shape (bare severity tag) |
| `trivy_misconfig.sarif.json` | SARIF | trivy IaC misconfig | hand-authored, non-CVE rule ID (`AVD-...`) to exercise the misconfig/CVE ASVS split |
| `grype_sca.sarif.json` | SARIF | grype directory/SBOM scan | hand-authored, `security-severity` CVSS-score form |
| `osv_scanner.json` | native JSON | osv-scanner | hand-authored, matches `--format json` schema (`pkg/models` shape confirmed against upstream source for P11.12) |
| `gitleaks.json` | gitleaks JSON | gitleaks secret detection | hand-authored, matches gitleaks' own report array shape |
| `trufflehog.jsonl` | trufflehog JSON Lines | trufflehog secret detection with live verification (P13.2) | hand-authored, matches trufflehog's `--json` shape (one object per line, not an array); `"Verified":true` exercises the `[VERIFIED: confirmed active credential]` tag end to end |
| `zap_dast.sarif.json` | SARIF | OWASP ZAP DAST (P11.7) | **synthetic placeholder** — representative of ZAP's Automation Framework `sarif-json` report template, not a live capture (see below) |

`trivy_vuln.sarif.json`, `grype_sca.sarif.json`, and `osv_scanner.json` deliberately all
flag the same `CVE-2021-23337` at the same package/location, in the actual shape each real
tool's location field takes (osv-scanner's `pkg@version (path)` vs. the others' bare path)
— this is the concrete cross-tool dedup case P11.8 exists for, and the fixture set is
authored to exercise it end to end rather than in isolation.

## The one gap: no live ZAP capture

Generating a *real* captured fixture for `zap_dast.sarif.json` requires actually running
OWASP ZAP's Automation Framework against a live target (the OWASP-recommended deliberately
vulnerable apps for this: **Juice Shop**, **WrongSecrets**, and **VAmPI** for the `api`
mode) — this environment has no container runtime available to do that, and fabricating a
claim of having done so would be worse than being explicit about the gap (the same
principle already applied to reachability/ASVS: no unverifiable claim beats a wrong one).

To replace this fixture with a real capture once Docker/Podman is available:

```bash
# Juice Shop (baseline/active) — see https://github.com/juice-shop/juice-shop
docker run -d -p 3000:3000 bkimminich/juice-shop
aegis scan dast http://localhost:3000 --mode active

# VAmPI (api mode) — see https://github.com/erev0s/VAmPI
docker run -d -p 5000:5000 erev0s/vampi
aegis scan dast http://localhost:5000 --mode api --api-definition http://localhost:5000/openapi.json
```

Copy the resulting SARIF report (`internal/security/dast.go`'s `zapReportFile` inside the
ZAP work directory Aegis creates per run) in as the new `zap_dast.sarif.json`, then
regenerate the golden file (see below) and review the diff before committing.

## Regenerating the golden file

After changing a fixture, a parser, `DedupFindings`, `assignASVS`, or `Baseline.Apply`:

```bash
AEGIS_EVAL_UPDATE=1 go test ./internal/security/... -run TestScanRegressionAcrossRecordedOutputs
```

Review the diff to `regression.golden.json` like any other golden-file change before
committing — a diff you don't understand is a regression, not a fixture update.
