package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/server"
	"github.com/spf13/cobra"
)

// doctorSeverity ranks one check's outcome for `aegis doctor`'s report.
type doctorSeverity int

const (
	doctorPass doctorSeverity = iota
	doctorWarn
	doctorFail
)

func (s doctorSeverity) label() string {
	switch s {
	case doctorPass:
		return "PASS"
	case doctorWarn:
		return "WARN"
	default:
		return "FAIL"
	}
}

// doctorCheck is one row of `aegis doctor`'s report. Fix is only rendered for
// warn/fail rows — it names the corrective config key or command, mirroring
// the wording style of SelectSandbox's fallback reasons and security.Resolve's
// unavailability reasons this command reuses.
type doctorCheck struct {
	Name     string
	Severity doctorSeverity
	Detail   string
	Fix      string
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Preflight self-diagnostic: provider, sandbox, scanners, guard, workdir, daemon",
		Long: "Runs every silent-misconfiguration check the P25 live-eval batch's fixes were each " +
			"a one-off instance of: provider reachability and model availability, configured-vs-" +
			"actually-active sandbox backend (P25.2), security scanner availability, output-guard " +
			"vs. thinking-model/small_model pairing (P25.3), session workdir allowlist posture " +
			"(P25.1), and whether a running daemon is reachable and in sync with the config on " +
			"disk. Every check but the last works standalone, with no daemon required — this is a " +
			"true preflight, safe to run before `aegis serve`. Exits non-zero if any check fails, " +
			"so it can gate scripts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(out, "%-4s %-20s %v\n", doctorFail.label(), "config", err)
				return fmt.Errorf("config failed to load")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			checks := runDoctorChecks(ctx, cfg)
			renderDoctorChecks(out, checks)

			failed := 0
			for _, c := range checks {
				if c.Severity == doctorFail {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
	return cmd
}

func renderDoctorChecks(w io.Writer, checks []doctorCheck) {
	for _, c := range checks {
		fmt.Fprintf(w, "%-4s %-20s %s\n", c.Severity.label(), c.Name, c.Detail)
		if c.Fix != "" && c.Severity != doctorPass {
			fmt.Fprintf(w, "     -> %s\n", c.Fix)
		}
	}
}

// runDoctorChecks runs every preflight check and returns them in report
// order. config.Load() having already succeeded (checked by the caller)
// means sandbox.Normalize() and the rest of Load's own validation already
// passed — a syntactically invalid config never reaches here.
func runDoctorChecks(ctx context.Context, cfg *config.Config) []doctorCheck {
	checks := []doctorCheck{
		doctorProviderCheck(ctx, cfg),
		doctorSandboxCheck(cfg),
		doctorScannerCheck(ctx, cfg),
		doctorGuardCheck(cfg),
		doctorWorkdirCheck(cfg),
	}
	return append(checks, doctorDaemonChecks(ctx, cfg)...)
}

// doctorProviderCheck checks the configured provider is actually reachable:
// for Ollama (or any loopback base_url), a live GET /api/tags plus the
// configured model actually being pulled; for a cloud provider, that its API
// key made it into the environment (config.Load already resolved
// cfg.Provider.APIKey via config.ProviderAPIKey).
func doctorProviderCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "provider"

	if base := ollamaNativeBase(cfg); base != "" {
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(rctx, http.MethodGet, base+"/api/tags", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return doctorCheck{
				Name: name, Severity: doctorFail,
				Detail: fmt.Sprintf("ollama unreachable at %s (%v)", base, err),
				Fix:    "start it (`ollama serve`), or fix provider.base_url",
			}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		_ = json.Unmarshal(body, &result)

		if cfg.Provider.Model != "" && cfg.Provider.Model != "auto" {
			found := false
			for _, m := range result.Models {
				if m.Name == cfg.Provider.Model ||
					strings.TrimSuffix(m.Name, ":latest") == cfg.Provider.Model ||
					strings.HasPrefix(m.Name, cfg.Provider.Model+":") {
					found = true
					break
				}
			}
			if !found {
				return doctorCheck{
					Name: name, Severity: doctorWarn,
					Detail: fmt.Sprintf("ollama reachable at %s but model %q is not pulled", base, cfg.Provider.Model),
					Fix:    fmt.Sprintf("ollama pull %s", cfg.Provider.Model),
				}
			}
		}
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("ollama reachable at %s, model %q available", base, cfg.Provider.Model),
		}
	}

	if cfg.Provider.APIKey == "" {
		envVar := map[string]string{"anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY"}[cfg.Provider.Default]
		if envVar == "" {
			envVar = "ANTHROPIC_API_KEY or OPENAI_API_KEY"
		}
		return doctorCheck{
			Name: name, Severity: doctorFail,
			Detail: fmt.Sprintf("provider %q has no API key in the environment", cfg.Provider.Default),
			Fix:    "export " + envVar,
		}
	}
	return doctorCheck{
		Name: name, Severity: doctorPass,
		Detail: fmt.Sprintf("provider %q configured, API key present", cfg.Provider.Default),
	}
}

// doctorSandboxCheck runs the same SelectSandbox the daemon uses at startup
// (and the subprocess swarm worker reconstructs, internal/cli/worker.go) so
// the configured-vs-actually-active sandbox backend gap P25.2 fixed for the
// daemon is also caught before the daemon ever starts.
func doctorSandboxCheck(cfg *config.Config) doctorCheck {
	const name = "sandbox"
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sb, fallback, reason, err := server.SelectSandbox(cfg.Sandbox, cwd, logger)
	if err != nil {
		return doctorCheck{
			Name: name, Severity: doctorFail,
			Detail: fmt.Sprintf("sandbox.backend %q failed to initialize: %v", cfg.Sandbox.Backend, err),
			Fix:    "fix sandbox.backend/sandbox.runtime, or unset sandbox.strict to allow a fallback",
		}
	}
	if sb != nil {
		defer sb.Close()
	}

	backend := cfg.Sandbox.Backend
	if backend == "" {
		backend = "local"
	}
	if fallback {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("configured backend %q is not active: %s", backend, reason),
			Fix:    "install/start the missing runtime, or set sandbox.backend: local to make this intentional",
		}
	}
	active := "local"
	if sb != nil {
		active = sb.Name()
	}
	detail := fmt.Sprintf("configured %q, active %q", backend, active)
	if active == "local" {
		detail += " — no isolation, commands run directly on the host"
	}
	return doctorCheck{Name: name, Severity: doctorPass, Detail: detail}
}

// doctorScannerCheck resolves every enabled security scanner's availability
// (security.Resolve — the same host/container/WSL probe `aegis security
// status`/GET /security/status use) and warns about any that are enabled
// but unavailable. Opt-in tools an operator hasn't turned on are silently
// skipped — an unconfigured DAST/zap scanner is expected, not a misconfig.
func doctorScannerCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "scanners"
	opts := security.OptionsFromConfig(cfg.Security)
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var unavailable []string
	enabledCount := 0
	for _, d := range security.Descriptors() {
		enabled := d.DefaultEnabled
		if tc, ok := cfg.Security.Tools[d.Name]; ok && tc.Enabled != nil {
			enabled = *tc.Enabled
		}
		if !enabled {
			continue
		}
		enabledCount++
		if method, _, _, reason := security.Resolve(rctx, d.Name, opts); method == security.MethodNone {
			unavailable = append(unavailable, fmt.Sprintf("%s (%s)", d.Name, reason))
		}
	}

	if enabledCount == 0 {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "no scanners enabled"}
	}
	if len(unavailable) > 0 {
		sort.Strings(unavailable)
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("%d/%d enabled scanners unavailable: %s", len(unavailable), enabledCount, strings.Join(unavailable, "; ")),
			Fix:    "install the missing binaries, or run `aegis security install <name>` for guided setup",
		}
	}
	return doctorCheck{Name: name, Severity: doctorPass, Detail: fmt.Sprintf("%d/%d enabled scanners available", enabledCount, enabledCount)}
}

// doctorGuardCheck warns about the P25.3 pairing: output_guard.mode: llm
// (the default) routing verdict calls at a thinking model with no
// provider.small_model set, which tripled turn time and leaked meta-text in
// the live eval before the guard.go/engine_build.go fixes shipped. Those
// fixes make the combination survivable, not free — this stays a warning so
// an operator adding a new thinking model later still gets pointed at
// small_model.
func doctorGuardCheck(cfg *config.Config) doctorCheck {
	const name = "output guard"
	if !cfg.OutputGuard.Enabled || !strings.EqualFold(cfg.OutputGuard.Mode, "llm") {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "disabled, or mode is not \"llm\""}
	}
	if cfg.Provider.SmallModel != "" {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("llm mode, verdicts routed to provider.small_model %q", cfg.Provider.SmallModel),
		}
	}
	if looksLikeThinkingModel(cfg.Provider) {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("output_guard.mode: llm targets thinking model %q with no provider.small_model set", cfg.Provider.Model),
			Fix:    "set provider.small_model to a fast non-thinking model, or output_guard.enabled: false",
		}
	}
	return doctorCheck{Name: name, Severity: doctorPass, Detail: "llm mode, model does not look like a thinking model"}
}

// looksLikeThinkingModel heuristically flags a provider config likely to hit
// the P25.3 failure mode: an explicit extended-thinking opt-in
// (provider.think/reasoning_effort), or a model name carrying a common
// reasoning-model marker. Best-effort by design — false negatives just mean
// a missed warning, never a wrong hard failure.
func looksLikeThinkingModel(p config.ProviderConfig) bool {
	if p.Think != nil && *p.Think {
		return true
	}
	if p.ReasoningEffort != "" {
		return true
	}
	name := strings.ToLower(p.Model)
	for _, marker := range []string{"thinking", "-deep", "deepseek", "-r1", "qwq", "o1-", "o3-"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// doctorWorkdirCheck reports the session-workdir-allowlist posture
// (config.ServerConfig.SessionWorkdirAllowlist): a no-op/no-risk default on
// a loopback-only bind (any existing directory is accepted as a session
// workdir — a local caller is already as trusted as a local shell user), but
// worth surfacing once AllowRemote is set since an empty allowlist there
// means every remote session is confined to the daemon's own workspace.
func doctorWorkdirCheck(cfg *config.Config) doctorCheck {
	const name = "workdir allowlist"
	if !cfg.Server.AllowRemote {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: "loopback-only bind — any existing directory is accepted as a session workdir",
		}
	}
	n := len(cfg.Server.SessionWorkdirAllowlist)
	if n == 0 {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: "server.allow_remote is set but server.session_workdir_allowlist is empty",
			Fix:    "add directories to server.session_workdir_allowlist if remote clients need a workdir outside the daemon's own workspace",
		}
	}
	return doctorCheck{Name: name, Severity: doctorPass, Detail: fmt.Sprintf("server.allow_remote set, %d allowlist entries", n)}
}

// doctorDaemonChecks is the one check group that needs a running daemon; it
// degrades to a WARN (not a FAIL) when none is reachable, since every check
// above already works without one. When a daemon is reachable, it also
// compares this shell's cwd against the daemon's own default workspace
// (P25.1: a session created with no explicit Workdir silently gets the
// daemon's workspace, not the caller's) and cross-checks the daemon's live
// sandbox-fallback state against what doctorSandboxCheck just computed from
// the config on disk — a mismatch there means the running daemon is stale
// relative to a config edit and needs a restart to pick it up.
func doctorDaemonChecks(ctx context.Context, cfg *config.Config) []doctorCheck {
	const name = "daemon"
	cl := client.NewFromConfig(cfg)
	hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := cl.Health(hctx); err != nil {
		return []doctorCheck{{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("no daemon reachable at %s", cfg.Server.Addr),
			Fix:    "start one with `aegis serve` (or `aegis`, which auto-starts one)",
		}}
	}
	checks := []doctorCheck{{Name: name, Severity: doctorPass, Detail: "reachable at " + cfg.Server.Addr}}

	status, err := cl.StatusInfo(ctx)
	if err != nil {
		return checks
	}

	if cwd, err := os.Getwd(); err == nil && status.Workspace != "" && !samePath(cwd, status.Workspace) {
		checks = append(checks, doctorCheck{
			Name: "daemon workspace", Severity: doctorWarn,
			Detail: fmt.Sprintf("daemon's default workspace is %q, this shell is in %q", status.Workspace, cwd),
			Fix:    "sessions created with no explicit workdir (aegis chat, MCP, ACP) will use the daemon's workspace — pass a workdir explicitly, or restart the daemon from this directory",
		})
	}

	if status.SandboxFallback {
		checks = append(checks, doctorCheck{
			Name: "daemon sandbox", Severity: doctorWarn,
			Detail: "the running daemon's active sandbox fell back to unsandboxed local: " + status.SandboxFallbackReason,
			Fix:    "fix the sandbox runtime, then restart the daemon to pick it up",
		})
	}
	return checks
}

// samePath compares two directory paths for equality, case-insensitively on
// Windows (its filesystem paths are case-insensitive, unlike Linux/macOS).
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
