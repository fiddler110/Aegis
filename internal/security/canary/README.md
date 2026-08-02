# multiscanner canary fixture

Not a project. This is the tiny, deliberately-vulnerable tree
`aegis security verify-image` unpacks into a scratch directory and scans with
every tool the built multiscanner image claims to carry, asserting each one
reports a **non-zero** number of findings.

The non-zero assertion is the whole point. A `--version` probe proves a binary
exists; it does not prove the binary can work. The failure this fixture exists
to catch is the one that has actually shipped twice here — a scanner that exits
cleanly and reports nothing because it never loaded its rules or its database
(the documented gosec and osv-scanner shape), or one whose `--version` passes
while every real invocation dies (njsscan after the semgrep removal). See P55.3.

Every planted credential is a published, non-functional example value — the
canonical AWS documentation key pair and obviously-synthetic tokens. Nothing
here is or ever was a live secret. Aegis's own scans will flag this directory;
that is expected, and is the fixture doing its job.

Each file is kept to a few lines, and exists to trip specific tools:

| file | tools it feeds |
|------|----------------|
| `Dockerfile` | hadolint, trivy (misconfig) |
| `k8s-deployment.yaml` | kubescape, trivy (misconfig) |
| `package-lock.json` | trivy (vuln), grype, osv-scanner, syft |
| `requirements.txt` | trivy (vuln), grype, osv-scanner, syft |
| `credentials.env` | gitleaks, trufflehog, trivy (secret) |
| `app.py` | bandit, opengrep |
| `server.js` | njsscan, opengrep |
| `Gemfile`, `Rakefile`, `config/`, `app/controllers/` | brakeman (which refuses anything that isn't a Rails app) |

Adding a tool to the image means adding whatever trips it here **and** an entry
in `canaryExpectations` (`internal/security/verify.go`), or `verify-image` will
report it as unverifiable rather than passing it silently.
