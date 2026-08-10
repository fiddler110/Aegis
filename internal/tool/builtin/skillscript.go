package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

// The bundled threat-modeling scripts, wrapped as first-class typed tools
// (P39.18).
//
// With per-phase tool narrowing and handle-based editing in place (P39.16),
// tool *selection* stopped being the failure mode on a 14B local model — every
// remaining stumble in the validation run was a malformed **argument**:
// `scaffold.py --framework` with the value simply omitted, and a run directory
// composed by hand. The model was assembling a command line as a *string*,
// which is the same class of failure `fill_marker` removed from editing: a
// format the model has to get exactly right, with no partial credit.
//
// The fix has the same shape as the two that worked. A typed schema is rendered
// by the harness rather than remembered by the model, so `--framework` becomes a
// required enum instead of a flag token that has to be followed by a value. An
// omitted or unknown framework is rejected here, before python is ever spawned,
// with the accepted values in the error — the way a wrong `fill_marker` index is
// answered with the list of markers that do exist.
//
// One tool per script, not one generic "run a skill script" tool. A generic tool
// would need a free-form `args` blob to cover five different argument sets,
// which is the string-composition failure again wearing a JSON hat; the whole
// point is that each script's arguments are *named and typed*. It also matches
// how every other builtin is written: one struct, one narrow schema.
//
// Scoping: these are NOT registered by builtin.Register. They are added to the
// registry only when a run has actually loaded a skill that bundles the scripts
// (see ThreatModelScriptTools' callers in internal/cli and internal/server), so
// they cost nothing on the ~50-tool default surface — the per-turn schema cost
// P62.6 is about. Within a drive, the per-phase tool lists in internal/drive
// narrow them further: only the setup phase sees recon/scaffold, only the
// assessment phase sees inventory.

// threatModelFrameworks are the framework keys scaffold.py accepts, mapped to
// the short name it uses in the analysis filename and the run-directory slug
// (`stride-a` is analysed as STRIDE-A but files as `stride`). Derived from
// scaffold.py's FRAMEWORKS table; the two aliases it also tolerates (`nist`,
// `800-154`) are deliberately left out of the enum, since an enum's job is to
// present one canonical spelling per choice.
var threatModelFrameworks = map[string]string{
	"stride":       "stride",
	"stride-a":     "stride",
	"linddun":      "linddun",
	"pasta":        "pasta",
	"trike":        "trike",
	"vast":         "vast",
	"nist-800-154": "nist-800-154",
}

// frameworkEnum is the ordered enum rendered into the scaffold schema and into
// every rejection message, so the model is told the accepted values by the same
// list the validator checks against.
var frameworkEnum = []string{"stride", "stride-a", "linddun", "pasta", "trike", "vast", "nist-800-154"}

// maxSkillScriptOutput caps what a script's stdout can add to the context. The
// recon digest is a few KB by design; a runaway one must not become the reason
// a phase overflows.
const maxSkillScriptOutput = 24000

// skillScriptTool runs one bundled skill script with argv built from a typed
// schema. The shared runner is an implementation detail — each instance is a
// separately named tool with its own schema, which is what the model sees.
type skillScriptTool struct {
	name     string
	script   string // file name inside skillDir, e.g. "scaffold.py"
	desc     string
	inSchema string

	root     string // workspace root, for path confinement and as the script's cwd
	skillDir string // where the bundled scripts live (workspace-relative or absolute)

	// build turns validated arguments into the argv that follows the script
	// path. A non-empty errMsg is a structural rejection: the script is never
	// spawned, so a missing required argument cannot reach it.
	build func(t *skillScriptTool, root string, input json.RawMessage) (args []string, errMsg string)

	// now is the clock the scaffold tool stamps a run directory with. Injected
	// so a test can pin the generated name.
	now func() time.Time
}

func (t *skillScriptTool) Name() string        { return t.name }
func (t *skillScriptTool) Description() string { return t.desc }

// Capability is execute for all five: each spawns a python subprocess. That
// matches how the shell tool these replace was gated, so swapping them in
// changes no permission posture — the win is the argument schema, not a
// relaxation.
func (t *skillScriptTool) Capability() tool.Capability { return tool.CapExecute }

func (t *skillScriptTool) InputSchema() json.RawMessage { return schema(t.inSchema) }

func (t *skillScriptTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	root := effectiveRoot(ctx, t.root)
	args, errMsg := t.build(t, root, input)
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
	}
	script := t.scriptPath(root)
	if _, err := os.Stat(script); err != nil {
		return tool.Result{Content: fmt.Sprintf("%s is not available: %s could not be found (the skill's bundled scripts are not materialized in this workspace)", t.name, filepath.ToSlash(script)), IsError: true}, nil
	}
	py := pythonInterpreter()
	if py == "" {
		return tool.Result{Content: fmt.Sprintf("%s needs a working python 3 interpreter on PATH and none was found", t.name), IsError: true}, nil
	}
	cmd := exec.CommandContext(ctx, py, append([]string{script}, args...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if len(text) > maxSkillScriptOutput {
		text = text[:maxSkillScriptOutput] + "\n…(output truncated)"
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return tool.Result{Content: text, IsError: true}, nil
	}
	if text == "" {
		text = fmt.Sprintf("%s completed with no output", t.name)
	}
	return tool.Result{Content: text}, nil
}

// scriptPath resolves the bundled script, allowing skillDir to be given either
// absolute or workspace-relative (the CLI resolves it one way, the daemon the
// other).
func (t *skillScriptTool) scriptPath(root string) string {
	dir := t.skillDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, t.script)
}

// pythonInterpreter mirrors drive.pythonExe: probe each candidate with
// `--version` so Windows' App-execution-alias shim — on PATH even with no
// interpreter installed — is not mistaken for python.
func pythonInterpreter() string {
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(p, "--version").Run(); err == nil {
			return p
		}
	}
	return ""
}

// ThreatModelScriptTools returns the typed wrappers for the threat-modeling
// skill's bundled scripts, for whichever of them skillDir actually ships — an
// older materialized skill build missing normalize_ids.py yields four tools
// rather than a tool that always fails. Returns nil when skillDir is empty or
// bundles none of them, so a caller can register unconditionally.
func ThreatModelScriptTools(root, skillDir string) []tool.Tool {
	if strings.TrimSpace(skillDir) == "" {
		return nil
	}
	all := []*skillScriptTool{
		{
			name:     "threat_model_recon",
			script:   "recon.py",
			desc:     "Run the threat-modeling skill's deterministic repo recon digest (recon.py) and return its stdout: git metadata, languages, dependency manifests, listeners with a suggested deployment class, entry points, config keys, security-infrastructure and egress signals, per-file symbols, and the top-level directory list that drives the Coverage Ledger. Read this digest instead of reading dozens of source files raw.",
			inSchema: `{"type":"object","properties":{"root":{"type":"string","description":"workspace-relative directory to scan; omit for the workspace root"},"full":{"type":"boolean","description":"lift the per-section truncation caps (much larger output)"},"max_files":{"type":"integer","minimum":1,"description":"maximum source files to scan (script default 8000)"},"json_path":{"type":"string","description":"workspace-relative path to also write the full facts as JSON"}},"required":[]}`,
			build:    buildReconArgs,
		},
		{
			name:     "threat_model_scaffold",
			script:   "scaffold.py",
			desc:     "Scaffold the seven threat-model suite files (scaffold.py) with real structure and one `<!-- PENDING: <section> -->` marker per fillable section. `framework` is required. Omit `run_dir` and the tool creates and returns the correctly named, timestamped run directory itself — do not compose that path or look up the date yourself. Safe to re-run: a file whose markers are already being filled is kept unless `force` is set.",
			inSchema: `{"type":"object","properties":{"framework":{"type":"string","enum":["stride","stride-a","linddun","pasta","trike","vast","nist-800-154"],"description":"threat-modeling framework; use \"stride\" unless STRIDE-A or another framework was explicitly requested"},"target":{"type":"string","description":"target slug for the run directory and file titles (e.g. \"aegis\", \"auth-service\"); omit to use the workspace directory name"},"run_dir":{"type":"string","description":"workspace-relative run directory; omit to create a correctly named timestamped one under .aegis/security/threat-model/"},"date":{"type":"string","description":"analysis date YYYY-MM-DD for the title block; omit to leave it PENDING"},"force":{"type":"boolean","description":"overwrite suite files even where filling has already started"},"quiet":{"type":"boolean","description":"print only the summary line"}},"required":["framework"]}`,
			build:    buildScaffoldArgs,
		},
		{
			name:     "threat_model_inventory",
			script:   "inventory.py",
			desc:     "Generate the run's `inventory.yaml` sidecar deterministically from the finished markdown (inventory.py). Always use this instead of hand-writing the sidecar — it derives ids and tiers from the documents, so it cannot drift from them. Set `check` to verify the existing sidecar still regenerates identically instead of writing it.",
			inSchema: `{"type":"object","properties":{"run_dir":{"type":"string","description":"workspace-relative threat-model run directory"},"check":{"type":"boolean","description":"verify the on-disk sidecar matches what the markdown implies, without writing"}},"required":["run_dir"]}`,
			build:    buildRunDirArgs("check", "--check"),
		},
		{
			name:     "threat_model_verify",
			script:   "verify.py",
			desc:     "Run the mechanical cross-file verifier (verify.py) over an assembled threat-model run directory: leftover skeleton syntax, component-name consistency, dataflow references, threat/coverage bijection, finding-id sequence, tier and prerequisite consistency, count agreement, hollow sections, and the evidence substance floor. Exits non-zero naming each failing check with file:line evidence.",
			inSchema: `{"type":"object","properties":{"run_dir":{"type":"string","description":"workspace-relative threat-model run directory"},"quiet":{"type":"boolean","description":"print only failures and the summary line"}},"required":["run_dir"]}`,
			build:    buildRunDirArgs("quiet", "--quiet"),
		},
		{
			name:     "threat_model_normalize_ids",
			script:   "normalize_ids.py",
			desc:     "Canonicalize threat and finding identifiers across a run directory (normalize_ids.py): strip invented `T<n>.<suffix>` forms and zero-padding back to the bare id the analysis defines, renumber `FIND-##` to a gapless sequence, and rewrite the coverage table and every cross-reference in lockstep. Use this instead of renumbering by hand. Idempotent; set `check` to report drift without writing.",
			inSchema: `{"type":"object","properties":{"run_dir":{"type":"string","description":"workspace-relative threat-model run directory"},"check":{"type":"boolean","description":"report identifier drift without rewriting anything"}},"required":["run_dir"]}`,
			build:    buildRunDirArgs("check", "--check"),
		},
	}
	var out []tool.Tool
	for _, t := range all {
		t.root, t.skillDir, t.now = root, skillDir, time.Now
		if _, err := os.Stat(t.scriptPath(root)); err != nil {
			continue // not bundled in this skill build
		}
		out = append(out, t)
	}
	return out
}

// --- argv builders -------------------------------------------------------- //
//
// Each builder validates before it renders. Every rejection names what was
// wrong AND what is accepted, so a second attempt has the answer in front of it
// rather than another guess — the fill_marker rule applied to arguments.

func buildReconArgs(t *skillScriptTool, root string, input json.RawMessage) ([]string, string) {
	var a struct {
		Root     string `json:"root"`
		Full     bool   `json:"full"`
		MaxFiles int    `json:"max_files"`
		JSONPath string `json:"json_path"`
	}
	if err := parseArgs(input, &a); err != nil {
		return nil, err.Error()
	}
	scanRoot := root
	if strings.TrimSpace(a.Root) != "" {
		abs, err := resolvePath(root, a.Root)
		if err != nil {
			return nil, fmt.Sprintf("root %q is outside the workspace: %v", a.Root, err)
		}
		scanRoot = abs
	}
	args := []string{scanRoot}
	if a.JSONPath != "" {
		abs, err := resolvePath(root, a.JSONPath)
		if err != nil {
			return nil, fmt.Sprintf("json_path %q is outside the workspace: %v", a.JSONPath, err)
		}
		args = append(args, "--json", abs)
	}
	if a.Full {
		args = append(args, "--full")
	}
	if a.MaxFiles > 0 {
		args = append(args, "--max-files", strconv.Itoa(a.MaxFiles))
	}
	return args, ""
}

func buildScaffoldArgs(t *skillScriptTool, root string, input json.RawMessage) ([]string, string) {
	var a struct {
		Framework string `json:"framework"`
		Target    string `json:"target"`
		RunDir    string `json:"run_dir"`
		Date      string `json:"date"`
		Force     bool   `json:"force"`
		Quiet     bool   `json:"quiet"`
	}
	if err := parseArgs(input, &a); err != nil {
		return nil, err.Error()
	}
	fw := strings.ToLower(strings.TrimSpace(a.Framework))
	if fw == "" {
		return nil, "framework is required — choose one of: " + strings.Join(frameworkEnum, ", ")
	}
	short, ok := threatModelFrameworks[fw]
	if !ok {
		return nil, fmt.Sprintf("unknown framework %q — choose one of: %s", a.Framework, strings.Join(frameworkEnum, ", "))
	}
	target := slugify(a.Target)
	if target == "" {
		target = slugify(filepath.Base(root))
	}
	if target == "" {
		target = "target"
	}
	runDir := strings.TrimSpace(a.RunDir)
	if runDir == "" {
		// The model never has to know the clock or the naming convention: the
		// timestamp came from a `date` shell call before this tool existed, and
		// a guessed one silently collides two same-day runs.
		runDir = filepath.ToSlash(filepath.Join(".aegis", "security", "threat-model",
			fmt.Sprintf("%s-%s-%s", short, target, t.now().Format("2006-01-02-1504"))))
	}
	abs, err := resolvePath(root, runDir)
	if err != nil {
		return nil, fmt.Sprintf("run_dir %q is outside the workspace: %v", runDir, err)
	}
	args := []string{abs, "--framework", fw}
	if target != "" {
		args = append(args, "--target", target)
	}
	if d := strings.TrimSpace(a.Date); d != "" {
		if !isoDateRE.MatchString(d) {
			return nil, fmt.Sprintf("date %q is not YYYY-MM-DD — omit it to leave the title block PENDING", a.Date)
		}
		args = append(args, "--date", d)
	}
	if a.Force {
		args = append(args, "--force")
	}
	if a.Quiet {
		args = append(args, "--quiet")
	}
	return args, ""
}

// buildRunDirArgs is the shape three of the five scripts share: a required
// run-directory positional plus one boolean flag.
func buildRunDirArgs(field, flag string) func(*skillScriptTool, string, json.RawMessage) ([]string, string) {
	return func(t *skillScriptTool, root string, input json.RawMessage) ([]string, string) {
		var a map[string]any
		if err := parseArgs(input, &a); err != nil {
			return nil, err.Error()
		}
		runDir, _ := a["run_dir"].(string)
		runDir = strings.TrimSpace(runDir)
		if runDir == "" {
			return nil, "run_dir is required: pass the threat-model run directory this suite lives in (the directory threat_model_scaffold reported)"
		}
		abs, err := resolvePath(root, runDir)
		if err != nil {
			return nil, fmt.Sprintf("run_dir %q is outside the workspace: %v", runDir, err)
		}
		args := []string{abs}
		if on, _ := a[field].(bool); on {
			args = append(args, flag)
		}
		return args, ""
	}
}

var isoDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// slugSanitizeRE keeps a target slug to characters that are safe in a directory
// name on every supported OS, so a model-chosen slug cannot produce a path the
// rest of the drive then cannot find.
var slugSanitizeRE = regexp.MustCompile(`[^a-z0-9._-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugSanitizeRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-.")
}
