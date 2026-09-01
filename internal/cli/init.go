package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelpick"
)

// runFirstInit writes the global config template to the user's OS config
// directory. It aborts if the file already exists to avoid clobbering changes,
// unless overwrite is set, in which case the existing file is backed up first.
//
// Before writing, it makes a short best-effort probe of the local Ollama
// instance (the template's active provider) and, if models are pulled,
// substitutes concrete provider.model / provider.small_model values in place
// of "auto" / the commented-out example — so a fresh install lands pinned to
// what is actually on the machine rather than a sentinel resolved later at
// runtime. A network hiccup or an empty Ollama library is not an error here;
// the template's existing "auto" defaults are already the correct fallback.
func runFirstInit(overwrite bool) error {
	path := config.GlobalConfigPath()
	template := applyOllamaTemplateDefaults(globalConfigTemplate)
	return writeConfigTemplate(path, template, "global", overwrite)
}

// statedVRAMBudget reads provider.vram_budget_gb out of the config that already
// exists, or 0 when there is none (the true first run) or it cannot be read.
//
// A re-run of --first-init --overwrite is the case this exists for. The budget
// is the operator's own answer to the question the model ranking is about to
// guess at, so ignoring it would mean the regenerated file ranks against a
// share of system RAM while the daemon plans residency against 14.5 GiB — two
// numbers for one machine. Worse, --overwrite rewrites from the template, so a
// budget that is not carried forward is *deleted by the run that should have
// used it*.
//
// Deliberately narrow: this is not --overwrite quietly becoming an `aegis
// config update`. Only the keys the ranking itself consumes are carried, and every
// other customization is still discarded, still diffed, and still backed up.
func statedVRAMBudget() (budget float64, autofit bool) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return 0, false
	}
	return cfg.Provider.VRAMBudgetGB, cfg.Provider.AutofitContext
}

// applyOllamaTemplateDefaults probes http://localhost:11434 for pulled models
// and, if any are found, rewrites the template so a fresh install lands
// configured for what is actually on the machine instead of generic
// placeholders the operator has to come back and fill in.
//
// Returns the template unchanged if Ollama isn't reachable or has no models:
// the template's existing "auto" defaults are already the correct fallback,
// and a network hiccup is not an error here.
func applyOllamaTemplateDefaults(template string) string {
	all, err := discoverOllamaModels("http://localhost:11434", 2*time.Second)
	if err != nil || len(all) == 0 {
		return template
	}
	return applyDiscoveredModelDefaults(template, all)
}

// applyDiscoveredModelDefaults ranks against whatever budget the existing
// config states; see statedVRAMBudget for why a re-run must not ignore it.
func applyDiscoveredModelDefaults(template string, all []ollamaModelInfo) string {
	budget, autofit := statedVRAMBudget()
	return applyModelDefaults(template, all, budget, autofit)
}

// applyModelDefaults is applyOllamaTemplateDefaults's pure half — no network,
// no config read — split out so the substitution rules are unit-testable
// against a synthetic model list instead of a live Ollama instance.
//
// The choices themselves belong to internal/modelpick, which ranks the pulled
// models rather than taking whichever one /api/tags happened to list first (the
// pre-P82 rule: most-recently-modified, so pulling a 3B for one experiment
// re-pinned the whole machine to it). What is decided here is only how a
// Selection becomes YAML:
//
//   - provider.model is pinned to Selection.Main instead of "auto".
//   - provider.small_model is uncommented with Selection.Small, when the
//     machine has a model meaningfully smaller than the main one.
//   - provider.think follows Selection.Think — the model's own advertised
//     "thinking" capability where Ollama reports one, else a name/family
//     heuristic. A wrong guess in either direction costs nothing: the native
//     adapter latches and silently drops `think` the first time a model 400s
//     on it (P52.5).
//   - output_guard.enabled flips to true, but *only* when small_model was
//     also set: the template's own documented reason it ships off is that the
//     guard's extra rubric call runs on the primary model without one, roughly
//     doubling turn latency for a local (often slow or thinking-style) model.
//     Setting small_model is exactly the condition that reasoning no longer
//     applies, so a two-model machine gets response validation for free
//     instead of a step the operator has to remember and come back for.
//
// Every pick is printed with the reason it was made, because an operator who
// disagrees with the ranking needs to know what it ranked on before they can
// override it.
func applyModelDefaults(template string, all []ollamaModelInfo, budgetGB float64, autofit bool) string {
	// budgetGB is 0 on a true first run — no config exists to have stated one —
	// and the ceiling then falls back to detected system RAM. See
	// modelpick.Ceiling for why that is a sanity bound rather than a fit.
	sel := modelpick.Select(pickModels(all), hwinfo.Detect(), budgetGB)
	fmt.Printf("Detected Ollama model%s: %s\n", pluralS(len(all)), strings.Join(modelNames(all), ", "))
	if sel.Main == "" {
		fmt.Printf("  None serve chat completions — leaving provider.model: \"auto\".\n")
		return template
	}

	template = strings.Replace(template,
		`model: "auto"              # "auto" picks the first available Ollama model`,
		fmt.Sprintf(`model: %q            # detected at "aegis --first-init" time; re-run with`, sel.Main)+"\n"+
			`                             # --overwrite after pulling new models, or edit by hand.`,
		1)

	if sel.Think {
		template = strings.Replace(template,
			`think: false               # true only for extended-thinking models such as
                             #   qwen3 or deepseek-r1 served via Ollama.`,
			`think: true                # detected at init time: `+sel.Main+` looks like a`+"\n"+
				`                             #   reasoning/thinking model. Set to false if it`+"\n"+
				`                             #   turns out not to be one, or answers ramble.`,
			1)
	}

	if sel.Small != "" {
		template = strings.Replace(template,
			`  # small_model: "llama3.2"  # Optional fast model for background calls`,
			fmt.Sprintf("  small_model: %q     # detected at init time: smallest suitable model pulled", sel.Small),
			1)
		// The template's own stated precondition for enabling the guard
		// without a latency penalty — see this function's doc comment — is
		// now met.
		template = strings.Replace(template,
			`output_guard:
  enabled: false             # validate each final answer; /guard on enables per session`,
			`output_guard:
  enabled: true              # validate each final answer; auto-enabled at init time since
                             # provider.small_model was detected (see it above) — verdicts
                             # route there instead of doubling latency on the primary model.
                             # /guard off disables per session.`,
			1)
	}

	if budgetGB > 0 {
		template = strings.Replace(template,
			"  # vram_budget_gb: 14.5",
			fmt.Sprintf("  vram_budget_gb: %g              # carried forward from your existing config", budgetGB),
			1)
		if autofit {
			template = strings.Replace(template,
				"  # autofit_context: false",
				"  autofit_context: true      # carried forward from your existing config",
				1)
		}
	}

	for _, r := range sel.Reasons {
		fmt.Printf("  %s\n", r)
	}
	if sel.Small != "" {
		fmt.Println("  output_guard enabled: rubric verdicts route to the small model, not the primary one.")
	}
	return template
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func modelNames(models []ollamaModelInfo) []string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.Name
	}
	return names
}

// runProjectInit writes a project-level override template to .aegis/config.yaml
// in the current working directory. It aborts if the file already exists,
// unless overwrite is set, in which case the existing file is backed up first.
func runProjectInit(overwrite bool) error {
	path := config.ProjectConfigPath()
	return writeConfigTemplate(path, projectConfigTemplate, "project", overwrite)
}

// diffFirstInit prints what an --overwrite of the global config would change,
// without writing anything — the --diff counterpart to runFirstInit, for
// deciding whether to overwrite (or what to copy forward by hand afterwards)
// before committing to it.
func diffFirstInit() error {
	path := config.GlobalConfigPath()
	template := applyOllamaTemplateDefaults(globalConfigTemplate)
	return diffConfigTemplate(path, template, "global")
}

// diffProjectInit is diffFirstInit's project-config counterpart.
func diffProjectInit() error {
	return diffConfigTemplate(config.ProjectConfigPath(), projectConfigTemplate, "project")
}

func diffConfigTemplate(path, template, label string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("No existing %s config at %s — nothing to diff. Run --first-init/--init to create one.\n", label, path)
		return nil
	} else if err != nil {
		return fmt.Errorf("check existing config: %w", err)
	}
	printConfigDiff(path, template, os.Stdout)
	return nil
}

// printConfigDiff prints every setting that differs between the config file
// at existingPath and newTemplate (existing value first, then what the fresh
// template would replace it with) — so `--overwrite` doesn't silently
// discard a customization the operator forgot they'd made, and `--diff` can
// answer "what would change" before committing to it.
//
// This is advisory only: a failure to read or parse either side is reported
// to out and swallowed rather than returned, since diffing must never be
// what stands between an operator and --overwrite actually writing the
// backup and the new file.
func printConfigDiff(existingPath, newTemplate string, out io.Writer) {
	oldCfg, err := config.LoadFileRaw(existingPath)
	if err != nil {
		fmt.Fprintf(out, "(could not read existing config to diff: %v)\n", err)
		return
	}
	tmp, err := os.CreateTemp("", "aegis-init-diff-*.yaml")
	if err != nil {
		fmt.Fprintf(out, "(could not diff against the new template: %v)\n", err)
		return
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(newTemplate)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		fmt.Fprintf(out, "(could not diff against the new template: %v)\n", firstNonNil(werr, cerr))
		return
	}
	newCfg, err := config.LoadFileRaw(tmp.Name())
	if err != nil {
		fmt.Fprintf(out, "(could not parse the new template to diff: %v)\n", err)
		return
	}
	diffs := config.Diff(oldCfg, newCfg)
	if len(diffs) == 0 {
		fmt.Fprintln(out, "No settings differ from the fresh template.")
		return
	}
	fmt.Fprintf(out, "%d setting(s) in the existing config differ from the fresh template (existing -> new):\n", len(diffs))
	for _, d := range diffs {
		fmt.Fprintf(out, "  %s\n", d)
	}
	fmt.Fprintln(out, "Anything you want to keep, copy forward by hand — the original is kept as a .bak file alongside it after --overwrite.")
}

func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func writeConfigTemplate(path, template, label string, overwrite bool) error {
	if existing, err := os.ReadFile(path); err == nil {
		if !overwrite {
			return fmt.Errorf("%s config already exists at %s\nEdit it directly, re-run with --overwrite to regenerate from the latest template (a backup is kept), or use `aegis config update` to merge in new fields without discarding customizations", label, path)
		}
		printConfigDiff(path, template, os.Stdout)
		backup := fmt.Sprintf("%s.bak-%d", path, time.Now().Unix())
		if err := os.WriteFile(backup, existing, 0o600); err != nil {
			return fmt.Errorf("write backup %s: %w", backup, err)
		}
		fmt.Printf("Backed up existing %s config to: %s\n", label, backup)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check existing config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(template), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Created %s config: %s\n", label, path)
	return nil
}

// ─── Global config template ───────────────────────────────────────────────────

// globalConfigTemplate and projectConfigTemplate are built from the raw
// templates below with the shared security-scanner addition block spliced
// in (see configupdate.go), so a fresh `--first-init`/`--init` and a
// reconciled `aegis config update` run against an older file converge on the
// same content.
//
// Both security blocks go through the single %s verb, joined here: a fresh
// --first-init must contain every addition `aegis config update` would splice
// into an older file, or the two paths diverge the moment a second addition
// ships.
var globalConfigTemplate = fmt.Sprintf(globalConfigTemplateRaw,
	securityScannerAdditionBlock+"\n\n"+securityContainerImagesAdditionBlock)

const globalConfigTemplateRaw = `# ═══════════════════════════════════════════════════════════════════════════════
# Aegis  ·  Global Configuration
# Generated by: aegis --first-init
#
# Precedence (highest wins):
#   environment variables  >  project config (.aegis/config.yaml)  >  this file
#
# API keys are NEVER read from this file — always set them as environment
# variables so they are not accidentally committed to source control.
# ═══════════════════════════════════════════════════════════════════════════════


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  ACTIVE: Ollama  (local, no API key required)
# └─────────────────────────────────────────────────────────────────────────────
#
# Aegis talks to Ollama's native /api/chat endpoint (provider.default: ollama),
# which needs NO API key — leave OPENAI_API_KEY unset. The native adapter sends
# a per-request context window (context_window below), controls keep_alive, and
# reads real token/load telemetry — none of which the older OpenAI-compat path
# (default: openai + a /v1 base_url) can do. That path silently served every
# request at Ollama's 4096 default, and the daemon warns against it.
#
# Ollama is started automatically if it is installed but not yet running.
# Model library:       https://ollama.com/library
#
provider:
  default: ollama
  base_url: "http://localhost:11434"
  model: "auto"              # "auto" picks the first available Ollama model
                             # automatically. Or pin a model you have pulled:
                             #   llama3.2, mistral, gemma3, qwen2.5, phi4, etc.
  max_tokens: 8192           # Cap on generated tokens per turn.
  max_retries: 3
  # context_window sets the per-request serving window (num_ctx) the native
  # adapter sends to Ollama, so Aegis's compaction threshold and Ollama's real
  # serving window agree. Larger = more context but more VRAM for the KV cache
  # — tune to your GPU (e.g. 8192 on a small card, 32768 on 16GB). Unset leaves
  # Ollama's own default (OLLAMA_CONTEXT_LENGTH or the modelfile value).
  # context_window: 32768
  # vram_budget_gb is how much memory you say the model server may hold across
  # EVERY concurrently resident model, in GiB. Stated, never detected — Aegis
  # does no GPU/VRAM introspection on any platform (see internal/hwinfo). It is
  # also not the card's capacity: subtract the driver reserve and whatever your
  # desktop already holds, e.g. 14.5 on a 16 GB card.
  #
  # Three things read it: "aegis models --fit" sizes context_window against it,
  # a debate plans its co-resident seats against it, and "aegis --first-init"
  # ranks which model to pin against it instead of falling back to a share of
  # system RAM. Stating it here is strictly better than any of them guessing.
  # vram_budget_gb: 14.5
  #
  # autofit_context lets the daemon re-solve context_window from vram_budget_gb
  # at startup, once the model has actually been loaded and its weights can be
  # measured. Leave it off to keep a window you tuned by hand.
  # autofit_context: false
  think: false               # true only for extended-thinking models such as
                             #   qwen3 or deepseek-r1 served via Ollama.
  # small_model: "llama3.2"  # Optional fast model for background calls
                             # (session titles, compaction, output-guard
                             # verdicts). Strongly recommended before enabling
                             # output_guard below — pick a small NON-thinking
                             # model you have pulled.
  # stream_idle_timeout raises the gap Aegis tolerates between two streamed
  # chunks before it gives up on a wedged model call (default: 10 minutes).
  # Raised here because local models can go quiet for a while mid-generation
  # on a slow GPU/CPU or a long thinking pass. cost.max_turn_stall below is
  # raised to match — it is the *other* bound on the same silence, and must
  # stay above this one or it cuts the run first (see CLAUDE.md).
  stream_idle_timeout: 1800   # 30 minutes


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  Anthropic  (Claude)          — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export ANTHROPIC_API_KEY=sk-ant-...
# Keys at:   https://console.anthropic.com/settings/keys
#
# provider:
#   default: anthropic
#   model: "claude-opus-4-8"   # claude-opus-4-8  |  claude-sonnet-4-6
#                              # claude-haiku-4-5-20251001
#   max_tokens: 16384
#   max_retries: 4
#   think: ~                   # ~ = provider default; false = disable thinking
#   # base_url: ""             # Override only when using a proxy / gateway.
#   # headers:                 # Extra headers for gateway auth, e.g.:
#   #   x-gateway-key: "token"


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  OpenAI                        — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=sk-...
# Keys at:   https://platform.openai.com/api-keys
#
# provider:
#   default: openai
#   model: "gpt-4o"            # gpt-4o  |  gpt-4o-mini  |  gpt-4-turbo
#                              # o1  |  o1-mini  |  o3  |  o3-mini
#   max_tokens: 16384
#   max_retries: 4
#   think: ~


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  Azure OpenAI                  — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=<your-azure-api-key>
# Replace <resource> and <deployment> with your Azure deployment details.
#
# provider:
#   default: openai
#   base_url: "https://<resource>.openai.azure.com/openai/deployments/<deployment>"
#   model: "gpt-4o"
#   max_tokens: 16384
#   max_retries: 4
#   headers:
#     api-version: "2024-05-01-preview"


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  Groq  (fast cloud inference)  — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=gsk_...
# Keys at:   https://console.groq.com/keys
#
# provider:
#   default: openai
#   base_url: "https://api.groq.com/openai/v1"
#   model: "llama-3.3-70b-versatile"
#                              # llama-3.3-70b-versatile  |  llama-3.1-8b-instant
#                              # mixtral-8x7b-32768  |  gemma2-9b-it
#   max_tokens: 8192
#   max_retries: 4
#   think: false


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  OpenRouter  (unified gateway)  — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=sk-or-...
# Keys at:   https://openrouter.ai/settings/keys
# Model list: https://openrouter.ai/models
#
# provider:
#   default: openai
#   base_url: "https://openrouter.ai/api/v1"
#   model: "anthropic/claude-opus-4"
#   max_tokens: 16384
#   max_retries: 4
#   headers:
#     HTTP-Referer: "https://github.com/fiddler110/Aegis"
#     X-Title: "Aegis"


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  LM Studio  (local)            — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=lmstudio
# Enable:    LM Studio → Settings → Local Server → Start Server
#
# provider:
#   default: openai
#   base_url: "http://localhost:1234/v1"
#   model: "local-model"       # Use the identifier shown in LM Studio's model list.
#   max_tokens: 4096
#   max_retries: 2
#   think: false


# ┌─────────────────────────────────────────────────────────────────────────────
# │  PROVIDER  ·  Google Vertex AI              — uncomment to activate
# └─────────────────────────────────────────────────────────────────────────────
# Requires:  export OPENAI_API_KEY=<vertex-api-key-or-bearer-token>
# Replace <project>, <region>, and <model-id> with your Vertex details.
#
# provider:
#   default: openai
#   base_url: "https://<region>-aiplatform.googleapis.com/v1beta1/projects/<project>/locations/<region>/endpoints/openapi"
#   model: "google/gemini-2.0-flash-001"
#   max_tokens: 8192
#   max_retries: 4


# ─────────────────────────────────────────────────────────────────────────────
#  Permission & behaviour
# ─────────────────────────────────────────────────────────────────────────────

permission:
  mode: build                # "plan" = read-only (safe default for untrusted dirs)
                             # "build" = file edits allowed; shell requires approval
  auto_approve_exec: false   # true = never prompt for shell/execute tool calls
  # rules:                   # Fine-grained allow/deny rules, evaluated before the
  #                          # mode gate. <tool> = name, alias (bash/write/read/
  #                          # network), or *. <pattern> is a glob; * spans "/".
  #   - "allow bash(npm test*)"   # auto-approve, no prompt
  #   - "deny write(/etc/*)"       # never write under /etc, even in auto mode
  #   - "deny shell(rm -rf /*)"


# ─────────────────────────────────────────────────────────────────────────────
#  Output validation  (off by default for local models; toggle per session with /guard)
# ─────────────────────────────────────────────────────────────────────────────
# The llm-mode guard makes one extra model call per final answer to judge it
# against a rubric. On a local setup that call runs on the same (often slow or
# thinking-style) model as the session unless provider.small_model is set, which
# roughly doubles turn latency for little signal. Set small_model above to a
# fast non-thinking model first, then flip enabled to true.

output_guard:
  enabled: false             # validate each final answer; /guard on enables per session
  mode: llm                  # "llm" (rubric check) or "schema" (required JSON keys)
  max_retries: 1             # corrective retries before surfacing the raw answer
  # rubric: |                # uncomment to override the built-in generic rubric
  #   The response must directly and completely address the request, contain no
  #   unfinished work (TODOs, stubbed-out logic), and ground factual claims in
  #   tool output. Clearly-marked example/placeholder values in documentation
  #   (e.g. an illustrative IP address) are fine.


# ─────────────────────────────────────────────────────────────────────────────
#  Per-persona model overrides  (blank = use the global provider.model above)
# ─────────────────────────────────────────────────────────────────────────────

personas:
  general:                     { model: "" }
  security:                    { model: "" }   # rec: claude-opus-4-8 — deep reasoning
  platform-architect:          { model: "" }   # rec: claude-opus-4-8
  security-architect:          { model: "" }   # rec: claude-opus-4-8 — threat modeling
  security-engineer:           { model: "" }
  appsec-engineer:             { model: "" }   # rec: claude-opus-4-8 — code review
  developer:                   { model: "" }   # rec: claude-sonnet-4-6 — fast iteration
  security-researcher:         { model: "" }
  risk-assessor:               { model: "" }
  business-analyst:            { model: "" }
  data-analyst:                { model: "" }   # rec: claude-opus-4-8
  network-security-architect:  { model: "" }
  report-writer:               { model: "" }   # rec: claude-sonnet-4-6 — long-form writing
  sre:                         { model: "" }
  infrastructure-architect:    { model: "" }
  cloud-architect:             { model: "" }
  cloud-security-engineer:     { model: "" }


# ─────────────────────────────────────────────────────────────────────────────
#  Spend guard  (cloud providers only — Ollama has no cost)
# ─────────────────────────────────────────────────────────────────────────────

cost:
  budget_usd: 0.0            # 0 = unlimited. Set e.g. 5.0 to abort past $5.00.
  # max_turn_stall aborts a turn after this many seconds of complete silence
  # (no streamed token, no tool activity). Default is 900 (15 min); raised
  # here to stay above provider.stream_idle_timeout above — see CLAUDE.md's
  # "the stall bound sits above every per-call timeout" invariant. Lower this
  # back down (or to 0 to disable) if you'd rather fail fast on a wedged run.
  max_turn_stall: 2100        # 35 minutes


# ─────────────────────────────────────────────────────────────────────────────
#  Daemon address
# ─────────────────────────────────────────────────────────────────────────────

server:
  addr: "127.0.0.1:4127"    # Change the port if 4127 is already in use.


# ─────────────────────────────────────────────────────────────────────────────
#  Logging
# ─────────────────────────────────────────────────────────────────────────────

log_level: info              # debug | info | warn | error


# ─────────────────────────────────────────────────────────────────────────────
#  Diagrams  (render_diagram tool)
# ─────────────────────────────────────────────────────────────────────────────

diagram:
  kroki_url: "https://kroki.io"
  # Self-host Kroki for air-gapped environments:
  # https://docs.kroki.io/kroki/setup/install/


# ─────────────────────────────────────────────────────────────────────────────
#  Multi-agent / swarm
# ─────────────────────────────────────────────────────────────────────────────

swarm:
  backend: in_process        # "in_process"  = goroutines, low overhead (default)
                             # "subprocess"  = process-isolated workers


# ─────────────────────────────────────────────────────────────────────────────
#  Shell execution sandbox
# ─────────────────────────────────────────────────────────────────────────────
# Defaults to "container": isolate each command in Docker/Podman. If no
# container runtime is available, falls back to "os" (macOS seatbelt / Linux
# bubblewrap, no container runtime needed). If OS-level isolation is
# unavailable too (every current Windows host, or a macOS/Linux box missing
# both), the daemon now refuses to start by default (sandbox.strict; see
# below) rather than silently falling back to unsandboxed "local" —
# P81.22/FIND-22. Set backend: local explicitly (and strict: false) once
# you've made the unsandboxed choice intentionally (P27.14/FIND-04).

sandbox:
  backend: container          # "container" = isolate each command in a chosen runtime
                             #               (Docker/Podman; falls back to os, then
                             #               local, if unavailable)
                             # "os"        = OS-level isolation, no container needed
                             #               (seatbelt/macOS, bwrap/Linux; falls back
                             #               to local if unavailable)
                             # "local"     = run commands directly on the host, unconfined
                             # "auto"      = detect & use the best available runtime,
                             #               same fallback cascade as "container"
  # runtime: ""              # Force a runtime when backend=container:
                             #   docker | podman | wslc (Windows WSL containers) | container (Apple)
                             #   Empty = auto-detect.
  # priority: [podman, docker, wslc]  # Auto-detect order; empty = OS default.
                             #   (Run: aegis sandbox detect  to see what's available.)
                             #   OS defaults: Windows [podman, docker, wslc];
                             #   macOS [docker, podman, container]; Linux [docker, podman].
                             #   wslc is last on Windows: no hardening flags, no
                             #   persistent-container support, and it can't build
                             #   the scanner images.
  # image: ubuntu:22.04      # Container image when backend=container/auto.
  # network: false           # Allow outbound network inside containers?
  # strict: true             # Default. Refuse to start rather than silently fall back to
                             #   unsandboxed "local" when no real isolation is available
                             #   (P81.22/FIND-22). Set false only on a host that genuinely
                             #   has neither Docker/Podman nor seatbelt/bwrap and accepts
                             #   running every command unconfined.
  # env_allow: []            # Extra env var names to pass through to sandboxed commands,
                             #   on top of the built-in allowlist (P81.26/FIND-26).


# ─────────────────────────────────────────────────────────────────────────────
#  Security policies
# ─────────────────────────────────────────────────────────────────────────────

security:
  egress_then_write: false   # true = require explicit approval for any file write
                             # that follows a network call in the same session.
  # network_allowlist:       # Restrict web_fetch / web_search to these domains.
  #   - "github.com"         # Empty list = no restriction (default).
  #   - "docs.python.org"
  #   - "pkg.go.dev"
%s


# ─────────────────────────────────────────────────────────────────────────────
#  LSP servers  (code intelligence — diagnostics, go-to-definition, etc.)
# ─────────────────────────────────────────────────────────────────────────────

# lsp:
#   - name: gopls
#     command: gopls
#     args: []
#     extensions: [".go"]
#
#   - name: pyright
#     command: pyright-langserver
#     args: ["--stdio"]
#     extensions: [".py"]
#
#   - name: rust-analyzer
#     command: rust-analyzer
#     args: []
#     extensions: [".rs"]
#
#   - name: typescript-language-server
#     command: typescript-language-server
#     args: ["--stdio"]
#     extensions: [".ts", ".tsx", ".js", ".jsx"]
#
#   - name: clangd
#     command: clangd
#     args: []
#     extensions: [".c", ".cpp", ".h", ".hpp"]


# ─────────────────────────────────────────────────────────────────────────────
#  MCP servers  (external tool servers via the Model Context Protocol)
# ─────────────────────────────────────────────────────────────────────────────

# mcp:
#   - name: filesystem
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/projects"]
#
#   - name: github
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-github"]
#     env:
#       GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_..."
#
#   - name: postgres
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/mydb"]
#
#   - name: brave-search
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-brave-search"]
#     env:
#       BRAVE_API_KEY: "BSA..."


# ─────────────────────────────────────────────────────────────────────────────
#  Process plugins  (external executables exposed as tools)
# ─────────────────────────────────────────────────────────────────────────────

# plugins:
#   - name: my_tool
#     description: "Run my custom analysis script"
#     command: python
#     args: ["/path/to/script.py"]
#     capability: execute        # read | write | execute | network
#     timeout_sec: 30
#     input_schema: >
#       {"type":"object","properties":{"target":{"type":"string"}},"required":["target"]}
`

// ─── Project config template ──────────────────────────────────────────────────

const projectConfigTemplate = `# ═══════════════════════════════════════════════════════════════════════════════
# Aegis  ·  Project Configuration
# Generated by: aegis --init
#
# This file overrides your global config (~/.config/aegis/config.yaml) for
# THIS PROJECT ONLY. Commit it to share settings with your team — it contains
# no secrets (API keys always come from environment variables).
#
# Precedence (highest wins):
#   environment variables  >  this file  >  global config
#
# The sections below appear in the same order as the global template, so the
# two files can be read side by side: every heading here has a counterpart
# there. The difference is what they contain. The global file states a whole
# posture with active values; this one ships every key COMMENTED OUT, because
# a project override is only worth writing when this repo genuinely differs
# from the machine's defaults. Uncomment a key and it wins for this directory;
# leave it commented and the global value stands.
#
# Three of the global file's sections have no counterpart here, and their
# absence is deliberate rather than an omission:
#   · server.addr and log_level are per-machine, not per-repo.
#   · security.dast.allowed_targets is never project-settable — a cloned repo
#     must not be able to name its own scan targets (the daemon logs and
#     ignores it if you try).
#   · multiscanner:/netscanner: image pins are machine-wide assets; see the
#     note under Security policies below.
# ═══════════════════════════════════════════════════════════════════════════════


# ─────────────────────────────────────────────────────────────────────────────
#  Provider  ·  model overrides for this project
# ─────────────────────────────────────────────────────────────────────────────
# Useful when a project needs a specific capability — a reasoning model for
# security analysis, a fast small one for routine edits, or a pinned tag so a
# team gets reproducible behaviour regardless of what each member has pulled.
#
# Only the keys you uncomment override; the rest of provider: comes from the
# global file. "auto" asks Aegis to rank what the local Ollama has pulled and
# take the best fit (the same ranking "aegis --first-init" uses), rather than
# whichever model happens to be listed first.
#
# provider:
#   model: "auto"              # or pin a tag: "qwen3:32b", "claude-opus-4-8"
#   # small_model: "llama3.2"  # fast model for titles, compaction, guard verdicts
#   # max_tokens: 32768
#   # think: ~                 # ~ = provider default; true/false to force
#   # context_window: 32768    # num_ctx sent to Ollama for this project's runs


# ─────────────────────────────────────────────────────────────────────────────
#  Permission & behaviour
# ─────────────────────────────────────────────────────────────────────────────
# Tighten the posture for a sensitive repo. A project may only NARROW what the
# global config allows here — it can move build -> plan, never plan -> build.
#
# permission:
#   mode: plan                 # "plan" = read-only: no file writes, no shell
#                              # "build" = edits allowed; shell needs approval
#   auto_approve_exec: false   # true = never prompt for shell/execute calls
#   # rules:                   # allow/deny rules, evaluated before the mode gate
#   #   - "allow bash(npm test*)"
#   #   - "deny write(/etc/*)"
#   #   - "deny shell(rm -rf /*)"


# ─────────────────────────────────────────────────────────────────────────────
#  Output validation
# ─────────────────────────────────────────────────────────────────────────────
# Worth enabling per-project when this repo's answers are deliverables (reports,
# threat models) rather than edits. Set provider.small_model above — globally or
# here — before turning it on, or the extra rubric call runs on the primary
# model and roughly doubles turn latency on a local setup.
#
# output_guard:
#   enabled: true
#   mode: llm                  # "llm" (rubric check) or "schema" (required keys)
#   max_retries: 1
#   # rubric: |
#   #   The response must directly and completely address the request and
#   #   ground every factual claim in tool output.


# ─────────────────────────────────────────────────────────────────────────────
#  Per-persona model overrides
# ─────────────────────────────────────────────────────────────────────────────
# Same shape as the global file's personas: block; name only the personas this
# project routes differently. Anything unlisted falls through to the global
# mapping, and from there to provider.model.
#
# personas:
#   security-architect: { model: "claude-opus-4-8" }
#   developer:          { model: "" }


# ─────────────────────────────────────────────────────────────────────────────
#  Spend guard
# ─────────────────────────────────────────────────────────────────────────────
# A project may only TIGHTEN a spend bound, never loosen one (P81.15): a value
# here that is larger than the global one is ignored and logged.
#
# cost:
#   budget_usd: 2.0            # abort a run past $2.00 of estimated cost
#   # max_tokens_per_run: 400000
#   # max_turn_stall: 2100     # seconds of complete silence before a turn aborts


# ─────────────────────────────────────────────────────────────────────────────
#  Multi-agent / swarm
# ─────────────────────────────────────────────────────────────────────────────
# swarm:
#   backend: subprocess        # "in_process" (default) or process-isolated


# ─────────────────────────────────────────────────────────────────────────────
#  Shell execution sandbox
# ─────────────────────────────────────────────────────────────────────────────
# sandbox:
#   backend: container         # container | os | local | auto
#   # runtime: podman          # force a runtime; empty = auto-detect
#   # image: ubuntu:22.04
#   # network: false


# ─────────────────────────────────────────────────────────────────────────────
#  Security policies
# ─────────────────────────────────────────────────────────────────────────────
# security:
#   egress_then_write: false   # true = approve any write that follows a fetch
#   network_allowlist:         # restrict web_fetch / web_search to these hosts
#     - "github.com"
#     - "pkg.go.dev"
#     - "docs.python.org"
#
#   # ── Per-scanner overrides (aegis scan / security_scan tool) ────────────
#   default_method: auto        # host | container | auto (default). Under "auto" a
#                               # tool the multiscanner image carries prefers the
#                               # CONTAINER over a host binary; a refused container
#                               # falls back to host and says so.
#   wsl_distro: kali-linux      # Windows only: target this WSL distro for nmap/nuclei/
#                               # opengrep/kubescape instead of the WSL default
#   tools:
#     trivy: { enabled: false }      # e.g. disable a noisy scanner for this repo
#     nmap:  { method: wsl }         # force WSL over a flaky native Windows install
#   dast:
#     allow_active: false
#   debate:
#     triage: false
#
# ── Do NOT put a multiscanner:/netscanner: block here ─────────────────────────
# The container scanner images and their shared database volume are machine-wide
# assets, so "aegis security build-image" pins them in your USER config
# (~/.config/aegis/config.yaml, %AppData%\aegis\config.yaml on Windows). Project
# config wins over user config, so an image_id copied into this file shadows the
# machine-wide pin and fails closed the first time the image is rebuilt:
# "no longer matches the ID recorded in config". Use "build-image --project" only
# when this repo deliberately runs a different image from the rest of the machine.
#
# ── security.dast.allowed_targets is deliberately absent ──────────────────────
# It is never project-settable. A cloned repository must not be able to name the
# hosts an active scan may be pointed at; set it in the user config or the
# environment. The daemon logs and ignores it if it appears here.


# ─────────────────────────────────────────────────────────────────────────────
#  Skills  (progressive-disclosure instructions; see: aegis skills list)
# ─────────────────────────────────────────────────────────────────────────────
# Built-in skills ship dormant in the binary and cost nothing until enabled.
# Project files in .aegis/skills/ are always active and need no entry here.
#
# skills:
#   builtin_enabled:
#     - document-codebase
#     - html-report


# ─────────────────────────────────────────────────────────────────────────────
#  Default persona for this project
# ─────────────────────────────────────────────────────────────────────────────
# persona: security-architect


# ─────────────────────────────────────────────────────────────────────────────
#  LSP servers  (code intelligence — diagnostics, references)
# ─────────────────────────────────────────────────────────────────────────────
# lsp:
#   - name: gopls
#     command: gopls
#     args: []
#     extensions: [".go"]


# ─────────────────────────────────────────────────────────────────────────────
#  MCP servers  (external tool servers via the Model Context Protocol)
# ─────────────────────────────────────────────────────────────────────────────
# mcp:
#   - name: github
#     command: npx
#     args: ["-y", "@modelcontextprotocol/server-github"]
#     env:
#       GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_..."


# ─────────────────────────────────────────────────────────────────────────────
#  Process plugins  (external executables exposed as tools)
# ─────────────────────────────────────────────────────────────────────────────
# plugins:
#   - name: my_tool
#     description: "Run my custom analysis script"
#     command: python
#     args: ["/path/to/script.py"]
#     capability: execute        # read | write | execute | network
#     timeout_sec: 30


# ─────────────────────────────────────────────────────────────────────────────
#  Notes  ·  what not to commit from .aegis/
# ─────────────────────────────────────────────────────────────────────────────
#   .aegis/sessions.db       — local session history
#   .aegis/daemon.token      — auth token for the local daemon
#   .aegis/mcp.token         — auth token for "aegis mcp-serve", auto-generated
#                              when AEGIS_MCP_TOKEN is unset (P27.4)
#   .aegis/acp.token         — auth token for "aegis acp", auto-generated
#                              when AEGIS_ACP_TOKEN is unset (P27.4)
#   .aegis/aegis.log         — daemon log
#   .aegis/.env              — secrets, read only in a TRUSTED workspace
#   .aegis/builtin-skills/   — built-in skills materialized into this project
#                              so their reference/skeleton assets are reachable
#                              by the model's sandboxed file tools; regenerated
#                              automatically, safe to delete
#
# Add these lines to your .gitignore if they are not already covered:
#   .aegis/sessions.db
#   .aegis/*.token
#   .aegis/*.log
#   .aegis/.env
#   .aegis/builtin-skills/
`
