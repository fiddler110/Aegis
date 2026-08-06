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

	"charm.land/lipgloss/v2"
	xterm "github.com/charmbracelet/x/term"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/modelcaps"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/server"
	"github.com/fiddler110/aegis/internal/toolcallprobe"
	"github.com/mattn/go-isatty"
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
	var deep bool
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
			"so it can gate scripts. --deep adds a slower, opt-in multi-turn probe (P39.4) that a " +
			"single-call tool-calling smoke test can't catch: a model that fabricates a completed " +
			"multi-file task instead of executing it, blanket-overwrites several identical " +
			"placeholder markers instead of targeting one, or never converges within a small turn " +
			"budget — the failure shapes a live scaffolded-skill run (e.g. threat-modeling) actually " +
			"hit on a model that passed the plain tool-calling check.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(out, "%-4s %-20s %v\n", doctorFail.label(), "config", err)
				return fmt.Errorf("config failed to load")
			}

			// 30s covers every other check plus one tool-calling trial; each
			// additional P53.4 trial buys its own slice, so raising the trial
			// count can't starve the rest of the report.
			timeout := 30 * time.Second
			if extra := doctorToolCallTrials(cfg) - 1; extra > 0 {
				timeout += time.Duration(extra) * doctorToolCallTrialTimeout
			}
			if deep {
				timeout += deepFillCheckTimeout
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			// Announced before the checks run, not after: the conformance
			// probe is the slow row and silence for minutes reads as a hang.
			if notice := doctorToolCallNotice(cfg); notice != "" {
				fmt.Fprintln(out, notice)
			}
			checks := runDoctorChecks(ctx, cfg)
			if deep {
				checks = append(checks, doctorDeepFillCheck(ctx, cfg))
			}
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
	cmd.Flags().BoolVar(&deep, "deep", false, "also run a slower, opt-in multi-turn structured-fill probe against the configured local model (see docs/providers.md)")
	return cmd
}

// renderDoctorChecks picks plain (unchanged, script/redirect-safe) or rich
// (colored, wrapped, severity-grouped) rendering depending on whether w is
// actually an interactive terminal. Piped/redirected output — including
// every test in doctor_test.go, which renders into a bytes.Buffer — always
// gets the plain form, so its exact layout (severity label at column 0, name
// as the second whitespace-separated field, a "-> fix" line immediately
// following) is a stable contract callers can parse.
func renderDoctorChecks(w io.Writer, checks []doctorCheck) {
	if f, ok := w.(*os.File); ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())) {
		renderDoctorChecksRich(w, checks, doctorTerminalWidth(f))
		return
	}
	renderDoctorChecksPlain(w, checks)
}

func renderDoctorChecksPlain(w io.Writer, checks []doctorCheck) {
	for _, c := range checks {
		fmt.Fprintf(w, "%-4s %-20s %s\n", c.Severity.label(), c.Name, c.Detail)
		if c.Fix != "" && c.Severity != doctorPass {
			fmt.Fprintf(w, "     -> %s\n", c.Fix)
		}
	}
}

// doctorTerminalWidth reads the real terminal width for wrapping, clamped to
// a readable range: wide enough to avoid ragged one-word-per-line wrapping
// on a narrow pane, capped so prose doesn't stretch edge-to-edge on an
// ultrawide terminal. Falls back to 100 when the ioctl fails (e.g. a pty
// that doesn't report size).
func doctorTerminalWidth(f *os.File) int {
	w, _, err := xterm.GetSize(f.Fd())
	if err != nil || w <= 0 {
		return 100
	}
	switch {
	case w > 120:
		return 120
	case w < 60:
		return 60
	default:
		return w
	}
}

// trimTrailingSpaces strips the right-padding lipgloss's Width() adds to
// every wrapped line (it pads short lines out to the box width) so a
// terminal's trailing-whitespace highlighting doesn't light up the whole
// report, and so piping through another tool doesn't carry invisible padding.
func trimTrailingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

var (
	doctorColorPass = lipgloss.Color("2")
	doctorColorWarn = lipgloss.Color("3")
	doctorColorFail = lipgloss.Color("1")
	doctorColorDim  = lipgloss.Color("8")
)

func (s doctorSeverity) badge() string {
	style := lipgloss.NewStyle().Bold(true)
	switch s {
	case doctorPass:
		return style.Foreground(doctorColorPass).Render("✓ PASS")
	case doctorWarn:
		return style.Foreground(doctorColorWarn).Render("! WARN")
	default:
		return style.Foreground(doctorColorFail).Render("✗ FAIL")
	}
}

// renderDoctorChecksRich renders a terminal-friendly report: a one-line
// summary, then failures and warnings first (grouped, wrapped to the
// terminal width, detail/fix on their own indented lines since those are
// often the longest strings in the report) followed by a compact list of
// passes. Severity order beats declaration order here because the whole
// point of a preflight report is "what needs my attention", and that's
// exactly what got lost in the flat, unwrapped list this replaces.
func renderDoctorChecksRich(w io.Writer, checks []doctorCheck, width int) {
	var fails, warns, passes []doctorCheck
	for _, c := range checks {
		switch c.Severity {
		case doctorFail:
			fails = append(fails, c)
		case doctorWarn:
			warns = append(warns, c)
		default:
			passes = append(passes, c)
		}
	}

	summary := fmt.Sprintf("%d check(s): ", len(checks))
	var parts []string
	if len(fails) > 0 {
		parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(doctorColorFail).Render(fmt.Sprintf("%d failed", len(fails))))
	}
	if len(warns) > 0 {
		parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(doctorColorWarn).Render(fmt.Sprintf("%d warning(s)", len(warns))))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(doctorColorPass).Render(fmt.Sprintf("%d passed", len(passes))))
	fmt.Fprintln(w, summary+strings.Join(parts, ", "))
	fmt.Fprintln(w, strings.Repeat("─", width))

	nameStyle := lipgloss.NewStyle().Bold(true)
	detailStyle := lipgloss.NewStyle().PaddingLeft(3).Width(width - 3)
	fixStyle := lipgloss.NewStyle().PaddingLeft(3).Width(width - 3).Foreground(doctorColorDim)

	for i, c := range append(append([]doctorCheck{}, fails...), warns...) {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  %s\n", c.Severity.badge(), nameStyle.Render(c.Name))
		fmt.Fprintln(w, trimTrailingSpaces(detailStyle.Render(c.Detail)))
		if c.Fix != "" {
			fmt.Fprintln(w, trimTrailingSpaces(fixStyle.Render("→ "+c.Fix)))
		}
	}

	if len(passes) > 0 {
		if len(fails)+len(warns) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, strings.Repeat("─", width))
		}
		nameW := 0
		for _, c := range passes {
			if len(c.Name) > nameW {
				nameW = len(c.Name)
			}
		}
		for _, c := range passes {
			fmt.Fprintf(w, "%s  %-*s  %s\n", c.Severity.badge(), nameW, c.Name, c.Detail)
		}
	}
}

// runDoctorChecks runs every preflight check and returns them in report
// order. config.Load() having already succeeded (checked by the caller)
// means sandbox.Normalize() and the rest of Load's own validation already
// passed — a syntactically invalid config never reaches here.
func runDoctorChecks(ctx context.Context, cfg *config.Config) []doctorCheck {
	checks := []doctorCheck{
		doctorWorkspaceTrustCheck(cfg),
		doctorProviderCheck(ctx, cfg),
		doctorProviderAdapterCheck(ctx, cfg),
		doctorGenerationBudgetCheck(ctx, cfg),
		doctorToolCallCheck(ctx, cfg),
		doctorSandboxCheck(cfg),
		doctorScannerCheck(ctx, cfg),
		doctorGuardCheck(cfg),
		doctorWorkdirCheck(cfg),
	}
	return append(checks, doctorDaemonChecks(ctx, cfg)...)
}

// doctorWorkspaceTrustCheck surfaces the P27.1 workspace-trust gate's
// outcome for the current directory: whether project-sourced
// permission.*/sandbox.*/mcp.servers/notify.webhook/hooks settings are
// currently frozen to their user/global values because this directory
// hasn't been explicitly trusted yet (`aegis trust`).
func doctorWorkspaceTrustCheck(cfg *config.Config) doctorCheck {
	const name = "workspace trust"
	if !cfg.WorkspaceTrust.Frozen {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "no untrusted project security overrides"}
	}
	return doctorCheck{
		Name: name, Severity: doctorWarn,
		Detail: fmt.Sprintf("%d project config change(s) frozen: %s", len(cfg.WorkspaceTrust.Changes), strings.Join(cfg.WorkspaceTrust.Changes, "; ")),
		Fix:    "run `aegis trust` to review and accept them",
	}
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

// doctorProviderAdapterCheck (P34.5) flags a config that reaches Ollama through
// the legacy OpenAI-compat adapter. It is a separate row from
// doctorProviderCheck deliberately: that check's ollama branch fires on a
// :11434 base_url regardless of provider.default, so it reports a cheerful
// "ollama reachable, model available" PASS for exactly the config this warns
// about — reachability is genuinely fine here, it's the adapter that isn't.
func doctorProviderAdapterCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "provider adapter"
	if !providerfactory.IsLegacyOllamaCompat(cfg.Provider) {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("provider %q uses its native adapter", cfg.Provider.Default),
		}
	}
	// Calibrate the recommended context_window against the model's real
	// training-context max (P35.3): the fixed fallback is a 16GB-VRAM-safe
	// number a skill-driven session overruns before writing any output. This is
	// best-effort — when the probe can't reach Ollama or read the max, the fix
	// falls back to the baseline recommendation.
	modelMax := doctorDetectModelMax(ctx, cfg.Provider)
	return doctorCheck{
		Name: name, Severity: doctorWarn,
		Detail: providerfactory.LegacyOllamaCompatDetail(cfg.Provider),
		Fix:    providerfactory.LegacyOllamaCompatFix(cfg.Provider, modelMax),
	}
}

// doctorGenerationBudgetCheck (P59.1) compares provider.max_tokens against the
// context window the model will actually be served with.
//
// On Ollama those are one budget — num_ctx covers prompt *and* completion — so
// a max_tokens at or above the window is not a generous cap, it is a request
// for more output than the whole conversation is allowed to occupy. The shipped
// default (32768) against a detected 4096 window is that case, and it is
// reachable from a stock install with no config at all. Nothing else reports
// it: config validation doesn't know the window (it is detected at runtime), and
// the failure it produces is silent — the model hits the ceiling mid-generation
// and the engine's continuation retry burns iterations while the answer quietly
// degrades. The adapter clamps the request so the run survives; this is the row
// that tells the user why their answers are short.
//
// Only meaningful on a shared-budget backend. Cloud providers bill max_tokens
// against a separate output allowance, so a large value there is correct.
//
// P61.4 sharpened it for the OpenAI-compat path, where it matters most: the
// native adapter always clamps, but the compat adapter can only clamp when the
// daemon managed to resolve a window for the model (see
// openai.clampMaxTokens) — a proxied Ollama, an unreachable server at startup,
// or any embedder that never sets Request.NumCtx leaves the pair unreconciled
// with nothing but this row to report it. That is also why a configured
// context_window is not accepted as the served window on that path — see
// doctorServedWindow.
func doctorGenerationBudgetCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "generation budget"
	if !isOllamaTarget(cfg.Provider) {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: "provider bills max_tokens against a separate output budget",
		}
	}

	window, source := doctorServedWindow(ctx, cfg.Provider)
	maxTokens := cfg.Provider.MaxTokens
	if window <= 0 {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("provider.max_tokens is %d; the served context window could not be determined", maxTokens),
		}
	}
	if maxTokens <= 0 {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("provider.max_tokens is unset; the model's own default applies within the %d-token window", window),
		}
	}

	// Half the window is the same line internal/engine's compaction trigger
	// draws: past it, more space is reserved for one answer than for the whole
	// conversation that produced it.
	if maxTokens*2 > window {
		sev := doctorWarn
		detail := fmt.Sprintf("provider.max_tokens (%d) claims most of the %d-token context window (%s) — on Ollama that window covers the prompt *and* the completion",
			maxTokens, window, source)
		if maxTokens >= window {
			detail = fmt.Sprintf("provider.max_tokens (%d) is >= the whole %d-token context window (%s) — on Ollama that window covers the prompt *and* the completion, so the model can be cut off mid-answer on its very first turn",
				maxTokens, window, source)
		}
		return doctorCheck{
			Name: name, Severity: sev, Detail: detail,
			Fix: doctorGenerationBudgetFix(cfg.Provider, window),
		}
	}
	return doctorCheck{
		Name: name, Severity: doctorPass,
		Detail: fmt.Sprintf("provider.max_tokens (%d) fits inside the %d-token context window (%s)", maxTokens, window, source),
	}
}

// isOllamaTarget reports whether requests go to an Ollama server, by either
// adapter — the native one or the legacy OpenAI-compat path, which has the same
// shared prompt+completion budget underneath.
func isOllamaTarget(p config.ProviderConfig) bool {
	return p.Default == "ollama" || providerfactory.IsLegacyOllamaCompat(p)
}

// doctorGenerationBudgetFix names the knob that actually moves, which is not
// the same knob on both adapters (P61.4). Quartering the window is the
// max_tokens half either way — it leaves three quarters for the conversation
// that produced the answer — but "raise provider.context_window" is advice the
// /v1 compat path cannot act on: that endpoint never sends num_ctx, so the
// value changes nothing about what Ollama serves. There the window moves on the
// server or by switching adapters, and the "provider adapter" row already
// spells the switch out in full.
func doctorGenerationBudgetFix(p config.ProviderConfig, window int) string {
	if providerfactory.IsLegacyOllamaCompat(p) {
		return fmt.Sprintf("set provider.max_tokens to at most %d — provider.context_window cannot help here, "+
			"the /v1 compat path never sends it; raise OLLAMA_CONTEXT_LENGTH on the server or switch to "+
			"provider.default: ollama (see the provider adapter row)", window/4)
	}
	return fmt.Sprintf("set provider.max_tokens to at most %d, or raise provider.context_window", window/4)
}

// doctorServedWindow returns the context window a request will actually carry
// and a short word for where the number came from; 0 means unknown.
//
// On the native adapter an explicit provider.context_window wins, because that
// is exactly what the adapter sends as num_ctx. On the OpenAI-compat path it
// deliberately does not (P61.4): that endpoint cannot carry num_ctx, so a
// configured window there is a statement of intent the server never hears.
// Trusting it would report a PASS for precisely the config that most needs this
// row — max_tokens 32768 against a configured 32768 window while Ollama serves
// its 4096 default — so the server's own reading is preferred, and when the
// server can't be reached but base_url is unambiguously Ollama, its documented
// out-of-the-box window stands in. That fallback is honest for a diagnostic in
// a way it would not be for a clamp: it costs a line of advice if wrong, not a
// truncated generation.
func doctorServedWindow(ctx context.Context, p config.ProviderConfig) (int, string) {
	compat := providerfactory.IsLegacyOllamaCompat(p)
	if p.ContextWindow > 0 && !compat {
		return p.ContextWindow, "provider.context_window"
	}
	if p.Model != "" && p.Model != "auto" {
		dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if res, ok := detectOllamaInfo(dctx, ollamainfo.NativeBase(p.BaseURL), p.Model); ok && res.ContextWindow > 0 {
			return res.ContextWindow, "detected"
		}
	}
	if compat && providerfactory.IsOllamaPortBaseURL(p.BaseURL) {
		return ollamainfo.DefaultServeContext, "Ollama's default; the /v1 compat path never sends context_window"
	}
	if p.ContextWindow > 0 {
		return p.ContextWindow, "provider.context_window"
	}
	return 0, ""
}

// detectOllamaInfo is a seam over ollamainfo.Detect so a test can supply a
// deterministic ModelMax without a live Ollama server, mirroring the
// selectSandbox seam.
var detectOllamaInfo = ollamainfo.Detect

// doctorDetectModelMax returns the configured model's training-context maximum
// from a reachable Ollama server, or 0 when the model is unresolved or the
// server can't be probed. Never fatal — a 0 result just yields the baseline
// context_window recommendation.
func doctorDetectModelMax(ctx context.Context, p config.ProviderConfig) int {
	if p.Model == "" || p.Model == "auto" {
		return 0
	}
	dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, ok := detectOllamaInfo(dctx, ollamainfo.NativeBase(p.BaseURL), p.Model)
	if !ok {
		return 0
	}
	return res.ModelMax
}

// doctorToolCallSmokePrompt is the obviously-actionable prompt sent to the
// model for the P28.2 tool-calling smoke test. The probe itself lives in
// internal/toolcallprobe so this check and the daemon's run-start gate (P34.2)
// share one definition; this alias is kept for the tests that assert on it.
const doctorToolCallSmokePrompt = toolcallprobe.SmokePrompt

// doctorToolCallCheck (P28.2, extended to a conformance rate by P53.4) does a
// live round-trip smoke test against the configured model: send an
// obviously-actionable prompt with one trivial tool schema and check whether
// the response contains at least one tool call — repeated
// provider.tool_call_probe_trials times, because "can this model ever call a
// tool" and "how often does it" are different questions and only the second
// one predicts whether an unattended run survives. Trials truncated at the
// token cap reach no verdict and are excluded from the rate's denominator
// rather than counted as misses (Run's P34.2 contract, preserved per trial).
// Because the trials run inline here, the command announces the sample size
// before starting (doctorToolCallNotice). Live evaluation (`TestLiveWorkflow`, 2026-07-14) found wide
// variance in local-model tool-calling reliability — `qwythos:latest` (this
// repo's own configured default) diagnosed a bug but never called
// edit_file/write_file to fix it, and `deepseek-r1:8b` made zero tool calls
// on an explicit task, answering in prose instead — while doctorProviderCheck
// above only verifies reachability and model availability, not tool-calling
// competence.
//
// Scoped to local (Ollama-style) providers only, the same gate
// doctorProviderCheck uses via ollamaNativeBase: this is where the observed
// variance lives, cloud providers (Anthropic/OpenAI) have well-established
// tool-calling support, and skipping them keeps this check free of network
// cost/latency for the common cloud-provider case. When no local provider is
// configured, or doctorProviderCheck's own reachability/model-availability
// check already failed for this config, this check silently skips (PASS,
// not a duplicate failure) rather than re-diagnosing the same gap — the same
// "unreachable/unconfigured provider is not this check's problem" pattern
// doctorProviderCheck itself follows for a missing cloud API key. Any
// failure past that point (timeout, transport error, malformed stream)
// degrades to WARN, never FAIL: a flaky or slow smoke test must never make
// `aegis doctor` non-zero-exit in an offline/CI context, matching how
// doctorDaemonChecks degrades to WARN rather than FAIL when no daemon is
// reachable.
func doctorToolCallCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "tool-calling"

	if ollamaNativeBase(cfg) == "" {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — only checked for local Ollama-style providers"}
	}
	if cfg.Provider.Model == "" || cfg.Provider.Model == "auto" {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — no model resolved yet"}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Reconcile before probing so this run's verdict is stamped with the digest
	// it was actually measured against — and so a model re-pulled since the
	// last run is re-probed here rather than reported from a stale record.
	caps := cfg.OpenModelCaps()
	modelcaps.ReconcileOllama(ctx, caps, ollamaNativeBase(cfg), cfg.Provider.Model, nil)
	adapter, err := providerfactory.Build(cfg, logger, providerfactory.WithModelCaps(caps))
	if err != nil {
		// doctorProviderCheck already reports provider misconfiguration.
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — provider not configured"}
	}

	trials := doctorToolCallTrials(cfg)
	rctx, cancel := context.WithTimeout(ctx, time.Duration(trials)*doctorToolCallTrialTimeout)
	defer cancel()

	conf, err := toolcallprobe.RunTrials(rctx, adapter, cfg.Provider.Model, trials)
	// Persist whatever verdict this sample reached (P53.5). doctor's sample is
	// the best one Aegis ever takes — fully blocking, full trial count — so
	// seeding the cache here means a daemon started afterwards reuses it
	// instead of re-probing on its first message.
	persistDoctorConformance(caps, cfg.Provider.Model, conf)
	if err != nil {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("tool-call smoke test could not run: %v", err),
			Fix:    "best-effort check, not fatal — retry `aegis doctor` once the model server is responsive",
		}
	}
	rate, ok := conf.Rate()
	if !ok {
		// Every trial was truncated: no verdict at all, never a 0% rate.
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("model %q hit the smoke test's %d-token cap before answering on all %d trial(s) — no verdict", cfg.Provider.Model, toolcallprobe.SmokeMaxTokens, conf.Trials),
			Fix:    "not a failure: the model ran out of tokens mid-answer, so the check proves nothing either way — common with reasoning models that think at length before acting",
		}
	}
	if conf.ToolCallTrials == 0 {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("model %q answered an obviously-actionable smoke-test prompt with zero tool calls — %s", cfg.Provider.Model, conf.Summary()),
			// Zero tool calls across the whole sample is the one verdict the
			// P53.6 shim is actually for — a model that cannot speak the
			// protocol, as opposed to one that speaks it inconsistently (the
			// rate<1 branch below, which the shim would not help). Naming it
			// here is the point of the check: the condition was detectable long
			// before there was anything to do about it.
			Fix: "either switch models — see docs/providers.md's \"Tool-calling reliability for local models\" section for families that have and haven't proven reliable — or set provider.tool_call_shim: on to serve the tool schemas in the prompt and parse calls out of the reply instead",
		}
	}
	// A partial rate is the signal this check exists to surface: a model that
	// complies on some trials and not others passes a single-trial probe and
	// then fails a long unattended run in a way that reads like a harness bug.
	if rate < 1 {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("model %q calls tools inconsistently — %s", cfg.Provider.Model, conf.Summary()),
			Fix:    "an unattended run needs consistency, not capability — see docs/providers.md's \"Tool-calling reliability for local models\" section, or raise provider.tool_call_probe_trials for a larger sample",
		}
	}
	return doctorCheck{
		Name: name, Severity: doctorPass,
		Detail: fmt.Sprintf("model %q — %s", cfg.Provider.Model, conf.Summary()),
	}
}

// persistDoctorConformance writes a doctor sample into the per-model
// capability cache (P53.5), if it reached a verdict at all.
//
// An all-truncated sample (Conformance.Verdict() == Unknown) is deliberately
// not written: the probe's contract is that a verdict it could not justify must
// never be cached, and persisting one would carry the mistake across restarts
// rather than only across sessions. A transport failure partway through still
// persists the trials that did land — a partial sample is real data.
func persistDoctorConformance(caps *modelcaps.Store, model string, conf toolcallprobe.Conformance) {
	if caps == nil || model == "" || conf.Trials == 0 {
		return
	}
	var verdict string
	switch conf.Verdict() {
	case toolcallprobe.OK:
		verdict = "ok"
	case toolcallprobe.Unsupported:
		verdict = "unsupported"
	default:
		return
	}
	modelcaps.ProbeStore{S: caps}.SetToolCalling(model, verdict, conf.Trials, conf.ToolCallTrials, conf.NoVerdict)
}

// doctorToolCallTrials is the sample size doctorToolCallCheck runs (P53.4),
// from provider.tool_call_probe_trials, defaulting to
// toolcallprobe.DefaultTrials when unset.
func doctorToolCallTrials(cfg *config.Config) int {
	if n := cfg.Provider.ToolCallProbeTrials; n > 0 {
		return n
	}
	return toolcallprobe.DefaultTrials
}

// doctorToolCallTrialTimeout bounds one trial. The whole check gets this per
// trial rather than one shared budget: an N-trial sample where trial 3 is slow
// shouldn't starve trials 4 and 5 into a transport error.
const doctorToolCallTrialTimeout = 20 * time.Second

// doctorToolCallNotice is the line `aegis doctor` prints before it starts,
// when the tool-calling check is going to run a multi-trial sample. Unlike the
// daemon, doctor runs the whole sample inline — that is up to N generations
// against a local model, and a command that goes quiet for minutes with no
// explanation reads as a hang. Returns "" when the check will skip or when a
// single trial makes it no slower than it has always been.
func doctorToolCallNotice(cfg *config.Config) string {
	if ollamaNativeBase(cfg) == "" || cfg.Provider.Model == "" || cfg.Provider.Model == "auto" {
		return ""
	}
	trials := doctorToolCallTrials(cfg)
	if trials <= 1 {
		return ""
	}
	return fmt.Sprintf("running the tool-calling conformance probe: %d trials against model %q (up to %s each) — set provider.tool_call_probe_trials=1 for the single-trial check",
		trials, cfg.Provider.Model, doctorToolCallTrialTimeout)
}

// deepFillCheckTimeout bounds doctorDeepFillCheck: it drives up to
// deepFillMaxTurns real model turns (internal/toolcallprobe.RunDeepFill),
// well beyond a single-request smoke test's budget. Measured generous rather
// than tight, the same way toolcallprobe.ProbeTimeout is: a live run against
// qwen3:14b (cold load ~18s, native Ollama, think disabled) on modest local
// hardware needed more than an initial 90s budget to complete several
// multi-turn fill rounds — a tight cap would misreport a merely-slow local
// model as a transport failure.
const deepFillCheckTimeout = 4 * time.Minute

// doctorDeepFillCheck (P39.4, --deep only) drives the configured local model
// through a tiny synthetic multi-section fill task and reports which of the
// three failure shapes the P38.1 live threat-modeling tests actually hit —
// fabricating a completed run instead of executing it (P38.6), blanket-
// overwriting several identical placeholder markers instead of targeting one
// (P38.7), or never converging within a small turn budget — were reproduced.
// A model can pass doctorToolCallCheck's single-call smoke test cleanly and
// still fail all three of these; they are a genuinely different capability
// claim ("can it drive a multi-turn scaffold-and-fill workflow"), so this is
// reported as its own row rather than folded into "tool-calling". Same gating
// and WARN-not-FAIL-on-transport-error posture as doctorToolCallCheck: scoped
// to local (Ollama-style) providers, skips (PASS) when unconfigured/
// unresolved, and any probe failure degrades to WARN, never FAIL.
func doctorDeepFillCheck(ctx context.Context, cfg *config.Config) doctorCheck {
	const name = "structured multi-turn fill"

	if ollamaNativeBase(cfg) == "" {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — only checked for local Ollama-style providers"}
	}
	if cfg.Provider.Model == "" || cfg.Provider.Model == "auto" {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — no model resolved yet"}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter, err := providerfactory.Build(cfg, logger)
	if err != nil {
		return doctorCheck{Name: name, Severity: doctorPass, Detail: "skipped — provider not configured"}
	}

	rctx, cancel := context.WithTimeout(ctx, deepFillCheckTimeout)
	defer cancel()

	res, err := toolcallprobe.RunDeepFill(rctx, adapter, cfg.Provider.Model)
	if err != nil {
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("structured-fill probe could not run: %v", err),
			Fix:    "best-effort check, not fatal — retry `aegis doctor --deep` once the model server is responsive",
		}
	}
	if res.Clean() {
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("model %q completed the synthetic multi-section fill task cleanly", cfg.Provider.Model),
		}
	}

	var shapes []string
	if res.FabricatedCompletion {
		shapes = append(shapes, "claimed completion without executing the work")
	}
	if res.ClobberedMarkers {
		shapes = append(shapes, "blanket-overwrote multiple sections instead of targeting one")
	}
	if res.TimedOut {
		shapes = append(shapes, "did not converge within the turn budget")
	}
	return doctorCheck{
		Name: name, Severity: doctorWarn,
		Detail: fmt.Sprintf("model %q failed the synthetic multi-section fill task: %s", cfg.Provider.Model, strings.Join(shapes, "; ")),
		Fix:    "this model may fail scaffolded, multi-file skills (e.g. threat-modeling) even though it passes the plain tool-calling check — see docs/providers.md's local-model guidance",
	}
}

// selectSandbox is a seam over server.SelectSandbox so tests can inject a
// deterministic sandbox-selection result without needing a real docker/podman
// install, mirroring internal/security's detectRuntime seam over
// sandbox.DetectBest. Without it the whole selection path
// (server.SelectSandbox -> sandbox.NewContainerBackend -> the real runtime
// probe) reaches the host, so a test asserting the runtime-absent branch
// really asserts "does this machine have podman?" — it passes precisely when
// it isn't exercising the misconfig it covers (P34.7).
var selectSandbox = server.SelectSandbox

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
	sb, fallback, reason, err := selectSandbox(cfg.Sandbox, cwd, logger)
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
		// FIND-04/P27.14: local gives no fs/network/process isolation — call
		// this out as a WARN (not a silent PASS) so it surfaces every run,
		// same as the daemon's own startup warning (server.New).
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: detail + " — no isolation, commands run directly on the host",
			Fix:    "consider sandbox.backend: os (macOS/Linux, no container runtime needed) or container for isolation of shell/execute tool calls",
		}
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
	cl, clErr := client.NewFromConfig(cfg)
	if clErr != nil {
		return []doctorCheck{{
			Name: name, Severity: doctorWarn,
			Detail: fmt.Sprintf("client config: %v", clErr),
			Fix:    "start the daemon at least once with `aegis serve` (or `aegis`) so it can generate its TLS certificate",
		}}
	}
	// cl is local to this check and never escapes — scrub its token once the
	// checks below are done (FIND-33/P24.21).
	defer cl.Zero()
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
