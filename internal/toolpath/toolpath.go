// Package toolpath resolves the external host commands Aegis can use, honoring
// user configuration before falling back to a PATH lookup.
//
// Several Aegis features run faster, or only run at all, when a host binary is
// present — ripgrep for search being the load-bearing example. Those lookups
// used to be scattered `exec.LookPath("rg")` calls, one of them a package-level
// var resolved at process init. That shape has three problems this package
// exists to fix:
//
//   - Not configurable. A user who installed the binary somewhere off PATH, or
//     under a different name (Debian ships fd as fdfind), had no way to point
//     Aegis at it. A *shell alias* is invisible either way: Aegis execs
//     binaries directly rather than through a shell, so `alias rg=...` in a
//     shell rc is never consulted — an explicit path in config is the fix.
//   - Not visible. Nothing told the user which optional tools were found, what
//     Aegis does with them, or what it silently falls back to without them.
//   - Not overridable downward. There was no way to force the pure-Go fallback
//     to reproduce a bug or compare backends.
//
// Resolution order for a key: an explicit config override (absolute path, bare
// name, or a disable keyword), then each registered candidate name on PATH.
package toolpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Spec describes one external command Aegis knows how to use.
type Spec struct {
	// Key is the config key under `commands:` and the name used in code.
	Key string
	// Candidates are binary names tried on PATH, in preference order. Debian
	// renames some tools (fd -> fdfind), hence a list rather than one name.
	Candidates []string
	// Purpose is what Aegis uses the tool for, shown by `aegis doctor`.
	Purpose string
	// Fallback is what happens when the tool is absent — shown so a user can
	// judge whether installing it is worth doing.
	Fallback string
	// Install maps a package manager to its install command, for `aegis doctor`
	// and for generating a host-provisioning script.
	Install map[string]string
}

// Registry is every external command resolvable through this package.
//
// Deliberately not exhaustive over everything Aegis ever execs: container
// runtimes (internal/sandbox) and security scanners (internal/security) have
// their own resolution with rules this package has no business overriding —
// runtime auto-detect order is OS-specific and security-relevant, and scanner
// resolution has to choose between container and host methods. This registry
// covers the general-purpose host tools that previously had no configurability
// at all.
var Registry = []Spec{
	{
		Key:        "ripgrep",
		Candidates: []string{"rg"},
		Purpose:    "backs the grep and glob tools; up to 6x faster than the built-in walker on a large tree",
		Fallback:   "a pure-Go directory walk — same results, but markedly slower on large trees",
		Install: map[string]string{
			"brew": "brew install ripgrep", "apt": "sudo apt-get install -y ripgrep",
			"dnf": "sudo dnf install -y ripgrep", "pacman": "sudo pacman -S --noconfirm ripgrep",
			"scoop": "scoop install ripgrep", "winget": "winget install BurntSushi.ripgrep.MSVC",
			"cargo": "cargo install ripgrep",
		},
	},
	{
		Key:        "git",
		Candidates: []string{"git"},
		Purpose:    "the git tool, commit/diff/log inspection, and checkpoint bookkeeping",
		Fallback:   "git-backed tools report an error; nothing else is affected",
		Install: map[string]string{
			"brew": "brew install git", "apt": "sudo apt-get install -y git",
			"dnf": "sudo dnf install -y git", "pacman": "sudo pacman -S --noconfirm git",
			"scoop": "scoop install git", "winget": "winget install Git.Git",
		},
	},
	{
		Key:        "gh",
		Candidates: []string{"gh"},
		Purpose:    "the git_pr tool (opening and reading GitHub pull requests)",
		Fallback:   "PR tools are unavailable; plain git still works",
		Install: map[string]string{
			"brew": "brew install gh", "apt": "sudo apt-get install -y gh",
			"dnf": "sudo dnf install -y gh", "pacman": "sudo pacman -S --noconfirm github-cli",
			"scoop": "scoop install gh", "winget": "winget install GitHub.cli",
		},
	},
	{
		Key:        "mmdc",
		Candidates: []string{"mmdc"},
		Purpose:    "local Mermaid rendering for the diagram tool",
		Fallback:   "diagrams render via the remote Kroki service instead (needs network)",
		Install:    map[string]string{"npm": "npm install -g @mermaid-js/mermaid-cli"},
	},
	{
		Key:        "plantuml",
		Candidates: []string{"plantuml"},
		Purpose:    "local PlantUML rendering for the diagram tool",
		Fallback:   "diagrams render via the remote Kroki service instead (needs network)",
		Install: map[string]string{
			"brew": "brew install plantuml", "apt": "sudo apt-get install -y plantuml",
			"scoop": "scoop install plantuml",
		},
	},
}

// SpecFor returns the registered spec for key.
func SpecFor(key string) (Spec, bool) {
	for _, s := range Registry {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// Well-known keys, so callers don't spell string literals at each use site.
const (
	Ripgrep  = "ripgrep"
	Git      = "git"
	GH       = "gh"
	Mermaid  = "mmdc"
	PlantUML = "plantuml"
)

// disableValues are config values that explicitly turn a tool off rather than
// naming a binary. Useful for reproducing a bug against the pure-Go fallback,
// and for pinning behavior on a machine where the host binary is a different
// version from everyone else's.
var disableValues = map[string]bool{
	"off": true, "false": true, "no": true, "none": true, "disabled": true, "0": true,
}

// Status is one resolved tool, for reporting.
type Status struct {
	Spec
	// Path is the resolved absolute binary path, empty when unavailable.
	Path string
	// Configured is the raw `commands:` value, empty when none was set.
	Configured string
	// Disabled is true when config explicitly turned the tool off.
	Disabled bool
	// Err explains why a tool is unavailable, empty when it resolved. A
	// configured-but-missing binary reports differently from one that was
	// simply never installed: the first is a mistake to correct, the second an
	// optional dependency the user may not want.
	Err string
}

// Available reports whether the tool can be executed.
func (s Status) Available() bool { return s.Path != "" }

// Resolver resolves tool keys to executable paths, caching each lookup.
// Safe for concurrent use.
type Resolver struct {
	overrides map[string]string
	mu        sync.Mutex
	cache     map[string]Status
}

// New builds a Resolver over the given `commands:` config overrides. A nil or
// empty map is fine and means "PATH lookup only".
func New(overrides map[string]string) *Resolver {
	norm := make(map[string]string, len(overrides))
	for k, v := range overrides {
		norm[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return &Resolver{overrides: norm, cache: make(map[string]Status)}
}

// Path returns the executable path for key, or "" when it is unavailable or
// disabled. Callers treat "" as "use the fallback".
func (r *Resolver) Path(key string) string { return r.Status(key).Path }

// Status resolves key and returns the full outcome, caching it. Resolution is
// cached because a PATH lookup is a filesystem walk and the search tools call
// this on every invocation; a tool installed mid-session is picked up on the
// next daemon start, same as before.
func (r *Resolver) Status(key string) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st, ok := r.cache[key]; ok {
		return st
	}
	st := r.resolve(key)
	r.cache[key] = st
	return st
}

func (r *Resolver) resolve(key string) Status {
	spec, known := SpecFor(key)
	if !known {
		spec = Spec{Key: key, Candidates: []string{key}}
	}
	st := Status{Spec: spec}

	if v, ok := r.overrides[key]; ok && v != "" {
		st.Configured = v
		if disableValues[strings.ToLower(v)] {
			st.Disabled = true
			st.Err = "disabled in config"
			return st
		}
		// An override that looks like a path is used as-is; a bare name still
		// goes through PATH, so `ripgrep: rg-14` works without an absolute path.
		if strings.ContainsAny(v, `/\`) || filepath.IsAbs(v) {
			if err := executable(v); err != nil {
				st.Err = "configured path is not executable: " + err.Error()
				return st
			}
			st.Path = v
			return st
		}
		if p, err := exec.LookPath(v); err == nil {
			st.Path = p
			return st
		}
		st.Err = "configured command " + v + " not found on PATH"
		return st
	}

	for _, name := range spec.Candidates {
		if p, err := exec.LookPath(name); err == nil {
			st.Path = p
			return st
		}
	}
	st.Err = "not found on PATH (" + strings.Join(spec.Candidates, ", ") + ")"
	return st
}

// executable checks that p exists and is a regular file we could run. The
// execute-bit test is POSIX-only; Windows decides executability by extension,
// which exec.Command already enforces at spawn time.
func executable(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &os.PathError{Op: "stat", Path: p, Err: os.ErrInvalid}
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return &os.PathError{Op: "exec", Path: p, Err: os.ErrPermission}
	}
	return nil
}

// Statuses resolves every registered tool, in registry order, for reporting.
func (r *Resolver) Statuses() []Status {
	out := make([]Status, 0, len(Registry))
	for _, s := range Registry {
		out = append(out, r.Status(s.Key))
	}
	return out
}

// UnknownKeys returns configured `commands:` keys that match no registered
// tool, sorted. A typo like `ripgrp: rg` would otherwise sit in the config
// doing nothing, which is exactly the kind of silent no-op that makes a knob
// look broken.
func (r *Resolver) UnknownKeys() []string {
	var out []string
	for k := range r.overrides {
		if _, ok := SpecFor(k); !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// InstallHint returns the install command for the manager most likely to work
// on this host, plus the manager's name. It prefers a manager actually present
// on the machine, and otherwise falls back to the platform's usual one so the
// hint is still actionable on a box with no package manager installed yet.
func (s Spec) InstallHint() (manager, command string) {
	var order []string
	switch runtime.GOOS {
	case "darwin":
		order = []string{"brew", "npm", "cargo"}
	case "windows":
		order = []string{"scoop", "winget", "npm", "cargo"}
	default:
		order = []string{"apt", "dnf", "pacman", "npm", "cargo"}
	}
	for _, m := range order {
		cmd, ok := s.Install[m]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(managerBinary(m)); err == nil {
			return m, cmd
		}
	}
	for _, m := range order {
		if cmd, ok := s.Install[m]; ok {
			return m, cmd
		}
	}
	return "", ""
}

func managerBinary(manager string) string {
	if manager == "apt" {
		return "apt-get"
	}
	return manager
}
