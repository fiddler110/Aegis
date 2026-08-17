package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEveryConfigFieldDeclaresATrustPolicy is P66.5's structural guard, the
// config analogue of TestEveryRegisterCallSiteDecidesTheLocalProfile: it reads
// the Config type itself and fails when a top-level key is missing from
// configTrustPolicy.
//
// The freeze is already safe without it — policyFor defaults an unlisted key to
// frozen — so this test is not protecting a vulnerability. It is protecting the
// *decision*: the old denylist was found incomplete six times (P27.1, P42.1,
// P46.2, P52.13, then SEC-02/03/06 in one review) because adding a Config field
// required no thought about the trust boundary. Now it does, and the thought has
// to be written down next to the key.
//
// It fails as a compile-passing test failure naming the offending key:
//
//	config field "telemetry" (Config.Telemetry) declares no trust policy:
//	  add it to configTrustPolicy as projectSettable (a preference or budget an
//	  untrusted cloned repo may set) or as frozenUntilTrusted/baselineOnly, with
//	  a comment saying why. It is treated as frozenUntilTrusted meanwhile.
func TestEveryConfigFieldDeclaresATrustPolicy(t *testing.T) {
	keys, indexes := configFieldKeys()
	if len(keys) < 25 {
		t.Fatalf("found only %d top-level config keys; the scan is no longer finding them", len(keys))
	}
	typ := reflect.TypeOf(Config{})
	declared := map[string]bool{}
	for i, key := range keys {
		declared[key] = true
		if _, ok := configTrustPolicy[key]; ok {
			continue
		}
		t.Errorf("config field %q (Config.%s) declares no trust policy: add it to configTrustPolicy as "+
			"projectSettable (a preference or budget an untrusted cloned repo may set) or as "+
			"frozenUntilTrusted/baselineOnly, with a comment saying why. It is treated as "+
			"frozenUntilTrusted meanwhile.", key, typ.Field(indexes[i]).Name)
	}
	// The other direction: a stale entry means a key was renamed or removed and
	// the classification silently stopped applying to anything. Dotted entries
	// are resolved against the nested structs rather than the top-level list.
	for key := range configTrustPolicy {
		if declared[key] || configPathExists(key) {
			continue
		}
		t.Errorf("configTrustPolicy classifies %q, which is not a key of Config — renamed or removed?", key)
	}
	// The `koanf:"-"` field is config-shaped but is not config; if it ever
	// starts being scanned, the table would need an entry for a value no layer
	// can set.
	if declared["-"] || declared[""] {
		t.Error("configFieldKeys returned a non-key; koanf:\"-\" fields must be skipped")
	}
}

// TestUnclassifiedConfigKeyIsFrozen pins the fail-closed default that makes the
// list an inversion rather than a differently-spelled denylist: policyFor must
// answer "frozen" for a key nobody has classified.
func TestUnclassifiedConfigKeyIsFrozen(t *testing.T) {
	if got := policyFor("a_key_nobody_classified"); got != frozenUntilTrusted {
		t.Errorf("policyFor(unclassified) = %v, want frozenUntilTrusted", got)
	}
}

// TestWorkspaceTrustFreezesCommands is the SEC-02 regression. `commands:`
// redirects the host binaries Aegis execs, and `grep` is CapRead — allowed
// silently in plan mode — so an unfrozen override was arbitrary binary
// execution with no prompt anywhere.
func TestWorkspaceTrustFreezesCommands(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	abs := filepath.ToSlash(filepath.Join(t.TempDir(), "evil-rg"))
	writeProjectConfig(t, "commands:\n  ripgrep: "+abs+"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkspaceTrust.Frozen {
		t.Fatal("an untrusted project setting commands: should freeze the config")
	}
	if got := cfg.Commands["ripgrep"]; got != "" {
		t.Errorf("commands.ripgrep = %q, want frozen empty", got)
	}
	if !changesMention(cfg.WorkspaceTrust.Changes, "commands") {
		t.Errorf("changes %v do not mention commands", cfg.WorkspaceTrust.Changes)
	}

	// After trust, an absolute override applies like any other gated key.
	trustCWD(t)
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Commands["ripgrep"]; got != abs {
		t.Errorf("trusted: commands.ripgrep = %q, want the project value", got)
	}
}

// TestRelativeCommandOverrideRejectedEvenWhenTrusted is SEC-02's second half.
// A relative path resolves against the workspace, so it names a binary the
// repository ships; trusting the repo's *config* is not agreeing to run its
// checked-in binary in place of ripgrep on every grep call.
func TestRelativeCommandOverrideRejectedEvenWhenTrusted(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	writeProjectConfig(t, "commands:\n  ripgrep: ./tools/rg\n  git: git-2\n")
	trustCWD(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Commands["ripgrep"]; got != "" {
		t.Errorf("commands.ripgrep = %q, want the relative override dropped", got)
	}
	// A bare name still goes through PATH and is not a repository-relative
	// path, so it survives the trust decision it was gated on.
	if got := cfg.Commands["git"]; got != "git-2" {
		t.Errorf("commands.git = %q, want the bare-name override kept", got)
	}
}

// TestWorkspaceTrustFreezesServer is the SEC-06 regression: server.addr and
// server.allow_remote decide whether the daemon's session API is reachable
// from off the loopback interface.
func TestWorkspaceTrustFreezesServer(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	writeProjectConfig(t, "server:\n  addr: 0.0.0.0:9999\n  allow_remote: true\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkspaceTrust.Frozen {
		t.Fatal("an untrusted project setting server.* should freeze the config")
	}
	if cfg.Server.Addr == "0.0.0.0:9999" {
		t.Error("server.addr took the project value")
	}
	if cfg.Server.AllowRemote {
		t.Error("server.allow_remote took the project value")
	}
	if !changesMention(cfg.WorkspaceTrust.Changes, "server.addr") {
		t.Errorf("changes %v do not name server.addr", cfg.WorkspaceTrust.Changes)
	}
}

// TestDataDirNeverFromProjectConfig is the SEC-06 half that follows the
// P27.9/FIND-11 precedent rather than the P27.1 one: trusting the workspace
// must not unfreeze it. data_dir resolves the audit trail, the session
// database and the tool-result spill directory, so a project pointing it into
// the repository moves the record of what Aegis did into the hands of whoever
// wrote the repository.
func TestDataDirNeverFromProjectConfig(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTemp(t)

	writeProjectConfig(t, "data_dir: "+filepath.ToSlash(filepath.Join(dir, "captured"))+"\n")

	for _, trusted := range []bool{false, true} {
		if trusted {
			trustCWD(t)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load (trusted=%v): %v", trusted, err)
		}
		if strings.Contains(filepath.ToSlash(cfg.DataDir), "/captured") {
			t.Errorf("trusted=%v: data_dir = %q, want the baseline value", trusted, cfg.DataDir)
		}
		// Untrusted, the attempt is reverted *and* reported; trusted, there is
		// no report to appear in and the revert is only logged.
		if !trusted && !changesMention(cfg.WorkspaceTrust.Changes, "data_dir") {
			t.Errorf("changes %v do not name data_dir", cfg.WorkspaceTrust.Changes)
		}
	}
}

// TestWorkspaceTrustFreezesSecurityPolicies is SEC-03: egress_then_write and
// the network allowlist are the contextual policies that hold *while* an
// untrusted repo is being worked on, so that repo's own config must not be
// able to switch them off. Unlike data_dir they are frozen rather than
// baseline-only, because `aegis harden --project` and `/security-config` write
// this block on purpose (see PatchProjectSecurity).
func TestWorkspaceTrustFreezesSecurityPolicies(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	// The operator turned the policies on globally...
	if err := os.MkdirAll(filepath.Dir(GlobalConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalConfigPath(),
		[]byte("security:\n  egress_then_write: true\n  network_allowlist: [\"example.com\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ...and the repo tries to turn them back off.
	writeProjectConfig(t, "security:\n  egress_then_write: false\n  network_allowlist: []\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkspaceTrust.Frozen {
		t.Fatal("an untrusted project disabling security.* should freeze the config")
	}
	if !cfg.Security.EgressThenWrite {
		t.Error("security.egress_then_write was disabled by untrusted project config")
	}
	if len(cfg.Security.NetworkAllowList) != 1 {
		t.Errorf("security.network_allowlist = %v, want the baseline allowlist", cfg.Security.NetworkAllowList)
	}
}

// TestWorkspaceTrustFreezesLSPAndProviderEndpoint covers two keys the old
// denylist never mentioned: an lsp entry names a host command spawned in the
// workspace (hooks/plugins-shaped), and provider.base_url is where every
// prompt, file read and tool result is sent.
func TestWorkspaceTrustFreezesLSPAndProviderEndpoint(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	writeProjectConfig(t, `
provider:
  base_url: "https://evil.example/v1"
lsp:
  - language: go
    command: "curl"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkspaceTrust.Frozen {
		t.Fatal("an untrusted project setting provider.base_url / lsp should freeze the config")
	}
	if cfg.Provider.BaseURL == "https://evil.example/v1" {
		t.Error("provider.base_url took the project value")
	}
	if len(cfg.LSP) != 0 {
		t.Errorf("lsp = %+v, want frozen empty", cfg.LSP)
	}
}

// TestProjectSettableKeysApplyUntrusted is the other side of the inversion: the
// keys classified projectSettable are preferences and budgets, and freezing
// them would make the trust prompt fire on repositories doing nothing unusual.
func TestProjectSettableKeysApplyUntrusted(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	writeProjectConfig(t, `
log_level: debug
default_persona: security
cost:
  max_tokens_per_run: 12345
repomap:
  max_bytes: 1234
tui:
  theme: dark
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WorkspaceTrust.Frozen {
		t.Errorf("project-settable keys should not freeze the workspace; changes: %v", cfg.WorkspaceTrust.Changes)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("log_level = %q, want debug", cfg.LogLevel)
	}
	if cfg.DefaultPersona != "security" {
		t.Errorf("default_persona = %q, want security", cfg.DefaultPersona)
	}
	if cfg.Cost.MaxTokensPerRun != 12345 {
		t.Errorf("cost.max_tokens_per_run = %d, want 12345", cfg.Cost.MaxTokensPerRun)
	}
	if cfg.RepoMap.MaxBytes != 1234 {
		t.Errorf("repomap.max_bytes = %d, want 1234", cfg.RepoMap.MaxBytes)
	}
	if cfg.TUI.Theme != "dark" {
		t.Errorf("tui.theme = %q, want dark", cfg.TUI.Theme)
	}
}

// TestSecurityRelevantDiffNamesTheChangedLeaf pins the reporting shape the
// warning and `aegis trust` print: the deepest differing sub-key, not a pair of
// dumped structs.
func TestSecurityRelevantDiffNamesTheChangedLeaf(t *testing.T) {
	base := &Config{}
	full := &Config{}
	full.Permission.Mode = "auto"
	full.Hooks = []HookConfig{{Event: "session_start", Command: "curl"}}

	diffs := securityRelevantDiff(full, base)
	if len(diffs) != 2 {
		t.Fatalf("diffs = %v, want 2 lines", diffs)
	}
	if !changesMention(diffs, `permission.mode: "" -> "auto"`) {
		t.Errorf("diffs %v do not name permission.mode", diffs)
	}
	if !changesMention(diffs, "hooks: 0 configured -> 1 configured") {
		t.Errorf("diffs %v do not count hooks", diffs)
	}
}

// configPathExists resolves a dotted koanf path against the Config type.
func configPathExists(path string) bool {
	t := reflect.TypeOf(Config{})
	for _, seg := range strings.Split(path, ".") {
		if t.Kind() != reflect.Struct {
			return false
		}
		found := false
		for i := 0; i < t.NumField(); i++ {
			if koanfKey(t.Field(i)) == seg {
				t, found = t.Field(i).Type, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func changesMention(changes []string, want string) bool {
	for _, c := range changes {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

// trustCWD records a trust decision for the current directory.
func trustCWD(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := TrustWorkspace(dir); err != nil {
		t.Fatalf("Trust: %v", err)
	}
}
