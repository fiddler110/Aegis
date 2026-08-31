package config

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
)

// trustPolicy says who is allowed to set one top-level config key.
type trustPolicy int

const (
	// projectSettable: a project's .aegis/config.yaml sets this freely,
	// trusted or not. Reserved for keys with no host-execution, network-
	// egress, confinement or audit-integrity effect.
	projectSettable trustPolicy = iota
	// projectMayTighten: a project's .aegis/config.yaml may make this bound
	// *stricter* than the operator's own layers resolve to, freely and with no
	// trust prompt, but may never loosen or remove it. A loosened value is
	// reverted to the baseline and logged (clampProjectLoosenedBounds).
	//
	// P81.15. The keys are the cost bounds. Freezing them outright was the
	// obvious alternative and is wrong: "this repo needs a smaller budget" is
	// an ordinary, harmless thing for a project to say, and making it raise a
	// trust prompt teaches the operator to click through the prompt that also
	// guards permission/sandbox/mcp. Leaving them projectSettable was the
	// status quo and is also wrong: the shipped cloud ceiling is not a bound
	// at all if any repository you clone can write `daily_cap_usd: 0`.
	//
	// Trust deliberately does not unlock loosening, which is what separates
	// this from frozenUntilTrusted. Trusting a repository's config is a
	// judgement about what that code may *do* on this machine; the spend
	// ceiling is a statement about the operator's own money, and a runaway
	// loop in a trusted repository bills exactly the same as one in an
	// untrusted repository. There is no trust decision to make here, so these
	// keys never enter securityRelevantDiff and never raise a prompt.
	projectMayTighten
	// frozenUntilTrusted: the project layer is discarded (the key falls back
	// to its user/global value) until an operator runs `aegis trust` here.
	frozenUntilTrusted
	// baselineOnly: the project layer is discarded unconditionally — trusting
	// the workspace does not unfreeze it. The P27.9/FIND-11 posture, for keys
	// whose blast radius reaches beyond the workspace that would be trusted.
	baselineOnly
)

// configTrustPolicy classifies every top-level key of Config.
//
// P66.5/SEC-02 (absorbing SEC-03, SEC-06). This used to be the other way
// round: an enumerated *denylist* of security-relevant keys, extended one key
// at a time as each omission was noticed — permission/sandbox/mcp/notify/hooks
// (P27.1), plugins (P42.1), git.pre_commit_test_command (P46.2),
// workspace.additional_roots (P52.13) — and still missing `commands`,
// `security.*`, `server.*` and `data_dir` when the P66 review looked. Six
// independent discoveries of one structural defect: a key added to Config is
// project-settable by default, and nothing makes anybody decide otherwise.
//
// So the list is inverted. The table below is exhaustive over Config's
// top-level fields, policyFor defaults an *unlisted* key to frozen, and
// TestEveryConfigFieldDeclaresATrustPolicy fails the build when a new field
// appears in neither column. The default already makes a forgotten field safe;
// the test is what makes "this key is fine for a cloned repo to set" a sentence
// somebody wrote rather than a line somebody never reached.
//
// Granularity is the whole top-level key by default. Classifying only
// `git.pre_commit_test_command` would be the same denylist one level down: the
// next dangerous key added under `git:` would be settable by default. So a
// struct is classified by its most dangerous member — `notify` is frozen for
// `webhook`'s sake, `git` for `pre_commit_test_command`'s — and a project needs
// trust to set the harmless siblings too, which is the right side to err on.
//
// A dotted entry refines a sub-tree, and the refinement is safe in exactly one
// direction: a sub-key is only listed to *narrow* the default, and anything
// under a frozen key that is not listed stays frozen. `provider` carries
// base_url and headers, which is why the block is frozen; naming
// `provider.model` settable keeps the ordinary "this repo wants qwen3:14b"
// case working without a trust prompt, and a provider field added tomorrow is
// frozen until someone lists it. `security.dast.allowed_targets` goes the other
// way, holding the stricter P27.9/FIND-11 posture inside a merely-frozen block.
var configTrustPolicy = map[string]trustPolicy{
	// ---- Never sourced from the project layer, trusted or not -------------
	//
	// data_dir (SEC-06) resolves the audit trail, the session database, the
	// swarm mailbox and the tool-result spill directory. A project pointing it
	// inside the repository moves the record of what Aegis did into the hands
	// of whoever wrote the repository — evidence relocation, not a preference,
	// and it survives the trust decision because the operator trusting a
	// workspace is not thereby agreeing to keep their history there.
	"data_dir": baselineOnly,
	// security.dast.allowed_targets is the original P27.9/FIND-11 case: the
	// authorization list for an *active* scanner. A project widening it aims
	// traffic at third-party hosts, which is past the boundary the operator
	// drew when they trusted this workspace, so trust does not unfreeze it.
	"security.dast.allowed_targets": baselineOnly,

	// ---- Frozen until `aegis trust` ---------------------------------------
	"permission": frozenUntilTrusted, // P27.1: mode/auto_approve_exec/rules are the gate itself
	"sandbox":    frozenUntilTrusted, // P27.1: backend/network decide what an approved command can reach
	"mcp":        frozenUntilTrusted, // P27.1: a server entry is an arbitrary host command plus a tool surface
	"hooks":      frozenUntilTrusted, // P27.1: a lifecycle command run on session_start, before any prompt
	"plugins":    frozenUntilTrusted, // P42.1: process tools, structurally identical to mcp
	"notify":     frozenUntilTrusted, // P27.1: webhook is an exfiltration channel
	"git":        frozenUntilTrusted, // P46.2: pre_commit_test_command shells out on every commit
	"workspace":  frozenUntilTrusted, // P52.13: additional_roots widens every tool's confinement boundary
	// commands (SEC-02) redirects the host binaries Aegis execs by key —
	// ripgrep, git, gh, mmdc, plantuml. `grep` is CapRead and therefore allowed
	// silently in *plan* mode, so an unfrozen `commands:` gave an untrusted repo
	// arbitrary binary execution through a read-capability tool with no prompt
	// anywhere. Relative-path values are rejected even after trust — see
	// rejectRelativeCommandOverrides.
	"commands": frozenUntilTrusted,
	// server (SEC-06) binds the daemon's HTTP API: addr and allow_remote decide
	// whether the session API is reachable off the loopback interface, and
	// trust_proxy_headers decides whether a forwarded header is believed.
	"server": frozenUntilTrusted,
	// lsp entries name a language-server *command* that is spawned in the
	// workspace — the same arbitrary-host-command shape as hooks and plugins,
	// and one the old denylist never covered.
	"lsp": frozenUntilTrusted,
	// mcp_server is the inbound direction (`aegis mcp-serve`): auto_approve
	// turns an external caller's tool call into an unprompted one, and
	// default_mode picks the permission mode it starts in.
	"mcp_server": frozenUntilTrusted,
	// security (SEC-03) holds egress_then_write, the network allowlist, secret
	// redaction, and how the scanners are executed (host vs. container, and
	// which image). These are the contextual policies that hold *while* a repo
	// is being worked on, so a cloned repo switching them off has cancelled the
	// protections that exist because it is untrusted. Frozen rather than
	// baseline-only because `aegis harden --project`, `/security-config` and
	// `aegis security build-image` all write this block on purpose — see
	// PatchProjectSecurity, which records trust the way PatchProjectSandbox
	// does for the same reason.
	"security": frozenUntilTrusted,
	// provider carries base_url, headers and the fallback chain. Every prompt,
	// file read and tool result travels to that endpoint, so a project pointing
	// it at a host it controls is notify.webhook's exfiltration channel at
	// conversation volume — and `default` picks which vendor (and which of the
	// operator's API keys) pays for it.
	"provider": frozenUntilTrusted,
	// ...but the ordinary reason a repo touches provider: is to pin a model and
	// its sampling/limit knobs for the work in that repo, which reaches no
	// endpoint the operator did not configure. Those are named here so the
	// common case does not need a trust prompt; everything else under
	// provider:, including anything added later, stays frozen.
	"provider.model":                   projectSettable,
	"provider.small_model":             projectSettable,
	"provider.max_tokens":              projectSettable,
	"provider.max_retries":             projectSettable,
	"provider.max_iterations":          projectSettable,
	"provider.loop_threshold":          projectSettable,
	"provider.zero_tool_nudge":         projectSettable,
	"provider.temperature":             projectSettable,
	"provider.seed":                    projectSettable,
	"provider.think":                   projectSettable,
	"provider.reasoning_effort":        projectSettable,
	"provider.context_window":          projectSettable,
	"provider.keep_alive":              projectSettable,
	"provider.response_header_timeout": projectSettable,
	"provider.stream_idle_timeout":     projectSettable,
	"provider.task_routing":            projectSettable,
	"provider.tool_call_probe_trials":  projectSettable,
	"provider.model_capabilities":      projectSettable,
	// Deliberately NOT listed, so they stay frozen: provider.vram_budget_gb,
	// provider.kv_cache_type (P69.6) and provider.autofit_context (P72.1). Every
	// knob above is a property of the *work* — which model, how many tokens, how
	// patient to be — which is a reasonable thing for a repo to pin. These are
	// properties of the operator's *machine*: how much VRAM the model server may
	// hold, and how its KV cache is quantized. A cloned repo declaring the first
	// oversizes every window on hardware it has never seen, which Ollama answers
	// by spilling to system RAM. No repo has that knowledge, so no repo gets to state it
	// untrusted. autofit_context joins them because it is the permission to
	// *act* on that budget — a repo that could set it could hand itself the
	// override of a hand-tuned context_window that flag exists to gate.
	// search.base_url/api_key send the model's search queries — and the user's
	// provider key for that service — to a project-chosen endpoint.
	"search": frozenUntilTrusted,
	// embeddings.base_url receives the content being indexed (knowledge base,
	// long-term memory) for the same reason.
	"embeddings": frozenUntilTrusted,
	// diagram.kroki_url receives diagram source, which is file content the
	// model chose to render.
	"diagram": frozenUntilTrusted,
	// cleanup deletes session history on a timer. A project shortening the TTL
	// to a day is deleting the record of its own run.
	"cleanup": frozenUntilTrusted,

	// ---- Project-settable --------------------------------------------------
	//
	// Everything here is a preference, a budget or a prompt-surface knob: it
	// grants no capability, reaches no host binary, and opens no egress the
	// operator has not already configured.
	"log_level":       projectSettable,
	"log":             projectSettable, // rotation size/backup count only, no capability grant
	"default_persona": projectSettable, // which built-in prompt a repo starts with
	"personas":        projectSettable, // per-persona model choice only
	"skills":          projectSettable, // enables Aegis's own embedded skills; project skill *files* are gated by trust elsewhere
	"repomap":         projectSettable, // byte/symbol budgets for the <repo_map> block
	"compaction":      projectSettable, // when and how old turns are summarized
	"output_guard":    projectSettable, // second-pass validation of Aegis's own output
	"tui":             projectSettable, // theme, keybindings, image rendering
	"swarm":           projectSettable, // in_process vs. subprocess sub-agents; the subprocess is Aegis itself
	"tools":           projectSettable, // additive deferred-tool families; permission still gates every call
	// cost is spend and run-length ceilings, and the block stays settable: a
	// repo whose build genuinely takes forty turns has a legitimate reason to
	// say so, and alert_threshold is a display preference.
	//
	// The bounds themselves are projectMayTighten (P81.15). The reasoning that
	// used to justify leaving them fully settable — "a project loosening its
	// own ceilings buys no capability the operator has not already granted" —
	// was true of the permission gate and false of the bill. It was written
	// when every one of these defaulted to 0/unlimited, so there was no
	// ceiling for a project to loosen; once a metered cloud endpoint ships a
	// real default, `daily_cap_usd: 0` in a cloned repo's config is the whole
	// ceiling removed by the party it exists to bound. Tightening stays free.
	"cost":                              projectSettable,
	"cost.budget_usd":                   projectMayTighten,
	"cost.max_tokens_per_run":           projectMayTighten,
	"cost.max_generated_tokens_per_run": projectMayTighten,
	"cost.max_wall_clock_per_run":       projectMayTighten,
	"cost.max_turn_stall":               projectMayTighten,
	"cost.session_cap_usd":              projectMayTighten,
	"cost.daily_cap_usd":                projectMayTighten,
	"cost.session_token_cap":            projectMayTighten,
	"cost.daily_token_cap":              projectMayTighten,
}

// policyFor returns the policy for a dotted config path: the path's own entry
// if it has one, otherwise the nearest classified ancestor's, otherwise frozen.
// The inversion is only real if the default is deny — a field added to Config
// without a table entry must be unavailable to an untrusted project, not
// silently available to it.
func policyFor(path string) trustPolicy {
	for p := path; ; {
		if pol, ok := configTrustPolicy[p]; ok {
			return pol
		}
		i := strings.LastIndex(p, ".")
		if i < 0 {
			return frozenUntilTrusted
		}
		p = p[:i]
	}
}

// hasRefinements reports whether any table entry classifies something *under*
// path, i.e. whether the walk has to descend rather than decide here.
func hasRefinements(path string) bool {
	prefix := path + "."
	for k := range configTrustPolicy {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// configFieldKeys returns Config's top-level koanf keys in declaration order,
// paired with their struct field index. Fields tagged `koanf:"-"` (today only
// the computed WorkspaceTrust status) are not config at all and are skipped.
func configFieldKeys() (keys []string, indexes []int) {
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		key := koanfKey(t.Field(i))
		if key == "" {
			continue
		}
		keys = append(keys, key)
		indexes = append(indexes, i)
	}
	return keys, indexes
}

// koanfKey returns the koanf name of a struct field, or "" when the field is
// unexported or explicitly excluded.
func koanfKey(f reflect.StructField) string {
	if f.PkgPath != "" {
		return ""
	}
	tag, _, _ := strings.Cut(f.Tag.Get("koanf"), ",")
	if tag == "" || tag == "-" {
		return ""
	}
	return tag
}

// walkConfigPolicy visits every point of Config at which the table makes a
// decision: a top-level key, or — where a dotted entry refines one — the
// sub-keys under it. Both the diff and the freeze below are this one walk, so
// the two cannot disagree about which keys are covered. The old code kept two
// hand-written lists (one per `if` in the diff, one assignment per key in the
// freeze), where a key added to either and not the other would have reported a
// change it did not prevent, or prevented one it did not report.
func walkConfigPolicy(cfg, baseline *Config, visit func(path string, pol trustPolicy, cfgv, basev reflect.Value)) {
	cv, bv := reflect.ValueOf(cfg).Elem(), reflect.ValueOf(baseline).Elem()
	keys, indexes := configFieldKeys()
	for i, key := range keys {
		walkPolicyValue(key, cv.Field(indexes[i]), bv.Field(indexes[i]), visit)
	}
}

func walkPolicyValue(path string, cfgv, basev reflect.Value, visit func(string, trustPolicy, reflect.Value, reflect.Value)) {
	if cfgv.Kind() == reflect.Struct && hasRefinements(path) {
		t := cfgv.Type()
		for i := 0; i < t.NumField(); i++ {
			sub := koanfKey(t.Field(i))
			if sub == "" {
				continue
			}
			walkPolicyValue(path+"."+sub, cfgv.Field(i), basev.Field(i), visit)
		}
		return
	}
	visit(path, policyFor(path), cfgv, basev)
}

// Diff returns a human-readable "key: from -> to" line for every leaf field
// that differs between from and to, across the whole Config — unlike
// securityRelevantDiff below, with no trust-policy filtering. Meant for
// operator-facing previews (e.g. `aegis --first-init --overwrite`/`--diff`
// showing what a regenerated config file would change) rather than the
// security gate, so every field is in scope, not just the frozen ones.
func Diff(from, to *Config) []string {
	var diffs []string
	keys, indexes := configFieldKeys()
	fv, tv := reflect.ValueOf(from).Elem(), reflect.ValueOf(to).Elem()
	for i, key := range keys {
		diffs = append(diffs, describeChanges(key, fv.Field(indexes[i]), tv.Field(indexes[i]))...)
	}
	return diffs
}

// securityRelevantDiff returns a human-readable line for each setting the
// project layer would change that it is not allowed to change — i.e. every
// non-project-settable key where full differs from base. The lines are what
// `aegis trust` and the untrusted-workspace warning show, so they name the
// deepest sub-key that actually differs rather than dumping whole structs.
func securityRelevantDiff(full, base *Config) []string {
	var diffs []string
	walkConfigPolicy(full, base, func(path string, pol trustPolicy, fullv, basev reflect.Value) {
		// projectMayTighten keys are absent here on purpose: they are handled
		// unconditionally by clampProjectLoosenedBounds, so they never need a
		// trust decision and must not add a line to a prompt that is asking
		// about permission/sandbox/mcp. See the policy's doc comment.
		if pol == projectSettable || pol == projectMayTighten {
			return
		}
		diffs = append(diffs, describeChanges(path, basev, fullv)...)
	})
	return diffs
}

// describeChanges renders "key: from -> to" for each leaf under key that
// differs, descending into structs so a one-field change reads as
// "permission.mode" rather than as two printed structs. Slices and maps are
// leaves: their element shape (an MCP server, an additional root) is not what
// the operator is being asked to judge, the count is.
func describeChanges(key string, from, to reflect.Value) []string {
	if reflect.DeepEqual(from.Interface(), to.Interface()) {
		return nil
	}
	if from.Kind() == reflect.Struct {
		var out []string
		t := from.Type()
		for i := 0; i < t.NumField(); i++ {
			sub := koanfKey(t.Field(i))
			if sub == "" {
				continue
			}
			out = append(out, describeChanges(key+"."+sub, from.Field(i), to.Field(i))...)
		}
		if len(out) > 0 {
			return out
		}
		// A struct whose fields are all untagged still changed somehow; fall
		// through rather than reporting nothing.
	}
	switch from.Kind() {
	case reflect.Slice, reflect.Map:
		return []string{fmt.Sprintf("%s: %d configured -> %d configured", key, from.Len(), to.Len())}
	default:
		return []string{fmt.Sprintf("%s: %s -> %s", key, formatValue(from), formatValue(to))}
	}
}

// formatValue renders one leaf for a diff line. Pointers are dereferenced
// because the tri-state knobs (provider.temperature, provider.think) would
// otherwise print as addresses.
func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return "unset"
		}
		return formatValue(v.Elem())
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	default:
		return fmt.Sprintf("%+v", v.Interface())
	}
}

// freezeToBaseline copies baseline's value over cfg's for every key whose
// policy want accepts.
func freezeToBaseline(cfg, baseline *Config, want func(trustPolicy) bool) {
	walkConfigPolicy(cfg, baseline, func(_ string, pol trustPolicy, cfgv, basev reflect.Value) {
		if want(pol) {
			cfgv.Set(basev)
		}
	})
}

// applyBaselineOnlyKeys discards the project layer for the baselineOnly keys.
// Unconditional: unlike the trust freeze it runs whether or not the workspace
// is trusted, and whether or not a project config file exists.
//
// It runs *after* applyWorkspaceTrust so that an untrusted project's attempt
// on one of these keys still appears in WorkspaceTrust.Changes — reverted by
// the freeze, and named in the warning like everything else it reverted. A
// trusted workspace has no such report, so the revert is logged instead rather
// than being a value the operator watches disappear.
func applyBaselineOnlyKeys(cfg, baseline *Config) {
	walkConfigPolicy(cfg, baseline, func(path string, pol trustPolicy, cfgv, basev reflect.Value) {
		if pol != baselineOnly || reflect.DeepEqual(cfgv.Interface(), basev.Interface()) {
			return
		}
		slog.Default().Warn("ignoring project config for a setting that is never project-settable",
			"key", path, "fix", "set it in ~/.config/aegis/config.yaml or the environment")
		cfgv.Set(basev)
	})
}

// clampProjectLoosenedBounds reverts every projectMayTighten bound the project
// layer made looser than the operator's own layers resolve to, and leaves the
// ones it made stricter alone.
//
// P81.15. Unconditional, like applyBaselineOnlyKeys and for a related reason:
// there is no trust decision to make (see projectMayTighten), so this runs
// whether or not the workspace is trusted and whether or not a project config
// file exists.
//
// It must run *after* applyCloudSpendDefaults, and baseline must be a config
// the same defaults have been applied to — via the project-excluded layer set,
// so "the operator's ceiling" means the one that would be in force with this
// repository absent. Ordering is the whole fix in the commonest case: a
// project writing `daily_cap_usd: 0` states a value koanf's Exists reports as
// set, which suppresses the shipped default, so before the defaults are
// resolved on both sides there is nothing here that even looks like a change.
func clampProjectLoosenedBounds(cfg, baseline *Config) {
	walkConfigPolicy(cfg, baseline, func(path string, pol trustPolicy, cfgv, basev reflect.Value) {
		if pol != projectMayTighten || !looserBound(cfgv, basev) {
			return
		}
		slog.Default().Warn("ignoring project config that would loosen a spend or run bound",
			"key", path,
			"project", formatValue(cfgv),
			"in_force", formatValue(basev),
			"fix", "raise it in ~/.config/aegis/config.yaml or the environment if this is intended")
		cfgv.Set(basev)
	})
}

// looserBound reports whether cfgv is a weaker bound than basev.
//
// Every key this is asked about documents `0 = unlimited` (or, for
// max_turn_stall, `0 = disabled`), which makes zero simultaneously the
// numerically smallest value and the semantically loosest one. A naive
// "smaller wins" comparison therefore reads `daily_cap_usd: 0` as the
// tightest possible ceiling and lets through precisely the value this exists
// to reject. Non-positive is normalized to unlimited on both sides, matching
// how MaxWallClockPerRun and MaxTurnStall already read these fields.
//
// Anything not numeric fails closed — reported as looser, and so reverted —
// so a bound added to the table with a shape this does not understand is
// conservative rather than silently unguarded.
func looserBound(cfgv, basev reflect.Value) bool {
	if reflect.DeepEqual(cfgv.Interface(), basev.Interface()) {
		return false
	}
	c, ok := boundMagnitude(cfgv)
	if !ok {
		return true
	}
	b, ok := boundMagnitude(basev)
	if !ok {
		return true
	}
	switch {
	case b <= 0:
		// The operator's own layers impose no bound, so nothing the project
		// can say is weaker than it.
		return false
	case c <= 0:
		// Unlimited against a real operator ceiling: the removal case.
		return true
	default:
		return c > b
	}
}

// boundMagnitude reads a bound field as a float, reporting false for a kind
// looserBound has no ordering for.
func boundMagnitude(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	default:
		return 0, false
	}
}

// rejectRelativeCommandOverrides drops any `commands:` value the project layer
// introduced that names a relative path.
//
// SEC-02's second half. A relative override is resolved against the process's
// working directory — the workspace — so `ripgrep: ./tools/rg` names a file the
// repository ships, and toolpath execs it directly. The trust freeze already
// keeps `commands:` out of an untrusted project's hands; this covers the
// trusted case, where "I trust this repo's config" should still not mean "I
// trust a checked-in binary to stand in for ripgrep on every grep call". An
// absolute path or a bare PATH name is unaffected, and so is a relative value
// that came from the user's own global config or environment (it is in the
// baseline, so it is not a project override).
func rejectRelativeCommandOverrides(cfg, baseline *Config) {
	for key, val := range cfg.Commands {
		v := strings.TrimSpace(val)
		if v == "" || !strings.ContainsAny(v, `/\`) || rooted(v) {
			continue // a bare name goes through PATH; a rooted path is explicit
		}
		if base, ok := baseline.Commands[key]; ok && base == val {
			continue // not project-sourced
		}
		slog.Default().Warn("ignoring relative commands: override from project config",
			"key", key, "value", val, "fix", "use an absolute path or a bare command name resolved on PATH")
		delete(cfg.Commands, key)
		if base, ok := baseline.Commands[key]; ok {
			cfg.Commands[key] = base
		}
	}
}

// rooted reports whether p starts at a filesystem root rather than at the
// process's working directory. filepath.IsAbs alone is not enough: on Windows
// it rejects "/usr/local/bin/rg", which is not workspace-relative either, and
// the question here is only whether the repository can place a file at the
// named path.
func rooted(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}
