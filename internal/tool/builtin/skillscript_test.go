package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

// bundledScripts are the five scripts P39.18 wraps as typed tools.
var bundledScripts = []string{"recon.py", "scaffold.py", "inventory.py", "verify.py", "normalize_ids.py"}

// scriptSkillDir builds a workspace with a materialized skill directory holding
// stub scripts, so the tools construct without needing python or the real
// bundle.
func scriptSkillDir(t *testing.T, scripts ...string) (root, skillDir string) {
	t.Helper()
	root = t.TempDir()
	rel := filepath.Join(".aegis", "builtin-skills", "threat-modeling")
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range scripts {
		if err := os.WriteFile(filepath.Join(dir, s), []byte("import sys\nsys.exit(0)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.ToSlash(rel)
}

func scriptTool(t *testing.T, tools []tool.Tool, name string) *skillScriptTool {
	t.Helper()
	for _, tl := range tools {
		if tl.Name() == name {
			st, ok := tl.(*skillScriptTool)
			if !ok {
				t.Fatalf("%s is %T, not *skillScriptTool", name, tl)
			}
			return st
		}
	}
	t.Fatalf("tool %q not registered; have %v", name, toolNames(tools))
	return nil
}

func toolNames(tools []tool.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name())
	}
	return out
}

// parsedSchema is the subset of JSON Schema these assertions care about.
type parsedSchema struct {
	Type       string `json:"type"`
	Required   []string
	Properties map[string]struct {
		Type string   `json:"type"`
		Enum []string `json:"enum"`
	}
}

func mustSchema(t *testing.T, tl tool.Tool) parsedSchema {
	t.Helper()
	var s parsedSchema
	if err := json.Unmarshal(tl.InputSchema(), &s); err != nil {
		t.Fatalf("%s: schema is not valid JSON: %v", tl.Name(), err)
	}
	if s.Type != "object" {
		t.Errorf("%s: schema type = %q, want object", tl.Name(), s.Type)
	}
	return s
}

// Every script gets its own named tool, and only for scripts the skill build
// actually bundles — a tool that would always fail is worse than an absent one.
func TestThreatModelScriptToolsOnePerBundledScript(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	got := toolNames(ThreatModelScriptTools(root, skillDir))
	want := []string{"threat_model_recon", "threat_model_scaffold", "threat_model_inventory", "threat_model_verify", "threat_model_normalize_ids"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	// An older materialized skill build missing a script yields fewer tools.
	root2, skillDir2 := scriptSkillDir(t, "recon.py", "scaffold.py")
	if got := toolNames(ThreatModelScriptTools(root2, skillDir2)); len(got) != 2 {
		t.Fatalf("partial bundle: tools = %v, want just recon+scaffold", got)
	}
	if got := ThreatModelScriptTools(root, ""); got != nil {
		t.Fatalf("no skill dir: tools = %v, want none", toolNames(got))
	}
}

// The whole point of P39.18: `--framework` stops being a token the model has to
// remember to follow with a value and becomes a required enum the harness
// renders.
func TestThreatModelScaffoldSchemaRequiresFrameworkEnum(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tl := scriptTool(t, ThreatModelScriptTools(root, skillDir), "threat_model_scaffold")
	s := mustSchema(t, tl)
	if strings.Join(s.Required, ",") != "framework" {
		t.Errorf("required = %v, want [framework]", s.Required)
	}
	fw, ok := s.Properties["framework"]
	if !ok {
		t.Fatal("schema has no framework property")
	}
	if strings.Join(fw.Enum, ",") != strings.Join(frameworkEnum, ",") {
		t.Errorf("framework enum = %v, want %v", fw.Enum, frameworkEnum)
	}
	for _, opt := range []string{"target", "run_dir", "date", "force", "quiet"} {
		if _, ok := s.Properties[opt]; !ok {
			t.Errorf("schema is missing optional property %q", opt)
		}
	}
	if tl.Capability() != tool.CapExecute {
		t.Errorf("capability = %q, want execute", tl.Capability())
	}
}

// The three run-directory scripts take a required positional in argparse, so it
// is required in the schema too — a schema that is looser than the script it
// wraps just moves the error later.
func TestThreatModelScriptSchemasMirrorArgparseRequirements(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tools := ThreatModelScriptTools(root, skillDir)
	for name, want := range map[string]string{
		"threat_model_inventory":     "run_dir",
		"threat_model_verify":        "run_dir",
		"threat_model_normalize_ids": "run_dir",
		"threat_model_recon":         "", // recon.py's root positional has a default
	} {
		s := mustSchema(t, scriptTool(t, tools, name))
		if strings.Join(s.Required, ",") != want {
			t.Errorf("%s required = %v, want %q", name, s.Required, want)
		}
	}
}

// The enum is derived from scaffold.py's own FRAMEWORKS table, not from
// memory: a framework added to (or dropped from) the script without the schema
// following is exactly the drift a typed tool is supposed to make impossible.
func TestFrameworkEnumMatchesScaffoldScript(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "skills", "builtin", "threat-modeling", "scaffold.py"))
	if err != nil {
		t.Skipf("bundled scaffold.py unavailable: %v", err)
	}
	block := regexp.MustCompile(`(?s)FRAMEWORKS = \{(.*?)\n\}`).FindSubmatch(src)
	if block == nil {
		t.Fatal("could not locate FRAMEWORKS table in scaffold.py")
	}
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([^"]+)":`).FindAllSubmatch(block[1], -1) {
		keys[string(m[1])] = true
	}
	for _, fw := range frameworkEnum {
		if !keys[fw] {
			t.Errorf("enum offers %q which scaffold.py does not accept", fw)
		}
	}
	// Every canonical key must be offered; the two aliases scaffold.py also
	// tolerates are deliberately not in the enum.
	aliases := map[string]bool{"nist": true, "800-154": true}
	for k := range keys {
		if aliases[k] {
			continue
		}
		if _, ok := threatModelFrameworks[k]; !ok {
			t.Errorf("scaffold.py accepts %q but the tool does not offer it", k)
		}
	}
}

// A flag and its value are rendered as one adjacent pair by the harness — the
// failure mode was `scaffold.py --framework` with nothing after it.
func TestThreatModelScaffoldRendersArgv(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tl := scriptTool(t, ThreatModelScriptTools(root, skillDir), "threat_model_scaffold")
	args, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{
		"framework": "stride-a", "target": "Auth Service", "run_dir": "run", "date": "2026-08-10", "force": true, "quiet": true,
	}))
	if errMsg != "" {
		t.Fatalf("build rejected a valid call: %s", errMsg)
	}
	if len(args) == 0 || args[0] != filepath.Join(root, "run") {
		t.Fatalf("argv[0] = %q, want the resolved run dir", args)
	}
	want := []string{"--framework", "stride-a", "--target", "auth-service", "--date", "2026-08-10", "--force", "--quiet"}
	if strings.Join(args[1:], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v after the run dir", args[1:], want)
	}
}

// With run_dir omitted the tool stamps the run directory itself, so the model
// never composes a path or reaches for `date`.
func TestThreatModelScaffoldCreatesTimestampedRunDirName(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tl := scriptTool(t, ThreatModelScriptTools(root, skillDir), "threat_model_scaffold")
	tl.now = func() time.Time { return time.Date(2026, 8, 10, 14, 32, 0, 0, time.UTC) }
	args, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{"framework": "stride-a", "target": "aegis"}))
	if errMsg != "" {
		t.Fatalf("build rejected: %s", errMsg)
	}
	// stride-a files as `stride`, mirroring scaffold.py's FRAMEWORKS short name.
	want := filepath.Join(root, ".aegis", "security", "threat-model", "stride-aegis-2026-08-10-1432")
	if args[0] != want {
		t.Errorf("run dir = %q, want %q", args[0], want)
	}
}

// An omitted required argument is rejected structurally: python is never
// spawned, so the script cannot see a malformed call at all.
func TestThreatModelScaffoldRejectsMissingFrameworkWithoutRunningScript(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join(".aegis", "builtin-skills", "threat-modeling")
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "script-ran")
	body := "open(r'''" + sentinel + "''','w').write('ran')\n"
	if err := os.WriteFile(filepath.Join(dir, "scaffold.py"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t, ThreatModelScriptTools(root, filepath.ToSlash(rel)), "threat_model_scaffold")

	for _, in := range []map[string]any{{}, {"framework": ""}, {"target": "aegis"}} {
		res, err := tl.Execute(context.Background(), mustJSON(t, in))
		if err != nil {
			t.Fatalf("Execute(%v) returned a hard error: %v", in, err)
		}
		if !res.IsError {
			t.Fatalf("Execute(%v) accepted a call with no framework: %q", in, res.Content)
		}
		if !strings.Contains(res.Content, "framework is required") {
			t.Errorf("Execute(%v) message = %q, want it to name the missing argument", in, res.Content)
		}
		// The rejection must enumerate what is accepted, the way a bad
		// fill_marker index is answered with the markers that do exist.
		for _, fw := range frameworkEnum {
			if !strings.Contains(res.Content, fw) {
				t.Errorf("rejection does not offer %q: %q", fw, res.Content)
			}
		}
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("scaffold.py ran despite a structurally invalid call")
	}
}

// A value outside the enum is refused the same way, rather than becoming
// scaffold.py's exit-2 usage error several seconds later.
func TestThreatModelScaffoldRejectsUnknownFramework(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tl := scriptTool(t, ThreatModelScriptTools(root, skillDir), "threat_model_scaffold")
	if _, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{"framework": "octave"})); !strings.Contains(errMsg, "unknown framework") {
		t.Errorf("errMsg = %q, want an unknown-framework rejection", errMsg)
	}
	if _, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{"framework": "stride", "date": "August 10"})); !strings.Contains(errMsg, "YYYY-MM-DD") {
		t.Errorf("errMsg = %q, want a date-format rejection", errMsg)
	}
}

func TestThreatModelRunDirToolsRequireRunDir(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tools := ThreatModelScriptTools(root, skillDir)
	for name, flag := range map[string]string{
		"threat_model_inventory":     "--check",
		"threat_model_normalize_ids": "--check",
		"threat_model_verify":        "--quiet",
	} {
		tl := scriptTool(t, tools, name)
		if _, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{})); !strings.Contains(errMsg, "run_dir is required") {
			t.Errorf("%s accepted an empty call: %q", name, errMsg)
		}
		field := "check"
		if flag == "--quiet" {
			field = "quiet"
		}
		args, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{"run_dir": "run", field: true}))
		if errMsg != "" {
			t.Fatalf("%s rejected a valid call: %s", name, errMsg)
		}
		want := []string{filepath.Join(root, "run"), flag}
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Errorf("%s argv = %v, want %v", name, args, want)
		}
		// The flag is opt-in, not implied.
		args, _ = tl.build(tl, root, mustJSON(t, map[string]any{"run_dir": "run"}))
		if len(args) != 1 {
			t.Errorf("%s argv = %v, want just the run dir when %s is unset", name, args, field)
		}
	}
}

func TestThreatModelReconArgv(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tl := scriptTool(t, ThreatModelScriptTools(root, skillDir), "threat_model_recon")
	args, errMsg := tl.build(tl, root, mustJSON(t, map[string]any{}))
	if errMsg != "" || len(args) != 1 || args[0] != root {
		t.Fatalf("default argv = %v (%s), want the workspace root alone", args, errMsg)
	}
	args, errMsg = tl.build(tl, root, mustJSON(t, map[string]any{
		"root": "sub", "full": true, "max_files": 250, "json_path": "facts.json",
	}))
	if errMsg != "" {
		t.Fatalf("build rejected: %s", errMsg)
	}
	want := []string{filepath.Join(root, "sub"), "--json", filepath.Join(root, "facts.json"), "--full", "--max-files", "250"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", args, want)
	}
}

// Every path argument stays inside the workspace: the tools are execute-class,
// so a path escape here would be a sandbox escape with a typed schema on it.
func TestThreatModelScriptToolsConfinePaths(t *testing.T) {
	root, skillDir := scriptSkillDir(t, bundledScripts...)
	tools := ThreatModelScriptTools(root, skillDir)
	for name, in := range map[string]map[string]any{
		"threat_model_scaffold": {"framework": "stride", "run_dir": "../escape"},
		"threat_model_verify":   {"run_dir": "../escape"},
		"threat_model_recon":    {"root": "../escape"},
	} {
		tl := scriptTool(t, tools, name)
		if _, errMsg := tl.build(tl, root, mustJSON(t, in)); !strings.Contains(errMsg, "outside the workspace") {
			t.Errorf("%s accepted an escaping path: %q", name, errMsg)
		}
	}
}
