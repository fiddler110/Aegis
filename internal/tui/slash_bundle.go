package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/bundle"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

func (d *SlashDispatcher) cmdSandbox(args []string) SlashResult {
	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	priority := sandbox.ParseRuntimes(cfg.Sandbox.Priority)

	// /sandbox use <target>: persist a new backend choice.
	if len(args) >= 2 && strings.ToLower(args[0]) == "use" {
		patch := config.SandboxPatch{Image: cfg.Sandbox.Image, Network: cfg.Sandbox.Network, Priority: cfg.Sandbox.Priority}
		target := strings.ToLower(strings.TrimSpace(args[1]))
		switch target {
		case "local", "auto":
			patch.Backend = target
		case "wsl", "wslc":
			patch.Backend, patch.Runtime = "container", "wslc"
		case "docker", "podman", "container":
			patch.Backend, patch.Runtime = "container", target
		default:
			return SlashResult{Output: fmt.Sprintf("Unknown sandbox target %q (want local, auto, docker, podman, wslc, or container).", args[1]), IsError: true}
		}
		if err := config.PatchGlobalSandbox(patch); err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to write config: %v", err), IsError: true}
		}
		return SlashResult{Output: fmt.Sprintf("Sandbox backend set to %q. Restart Aegis to apply.", target)}
	}

	// /sandbox: show current config and detected runtimes.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	backend := cfg.Sandbox.Backend
	if backend == "" {
		backend = "local"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sandbox backend: %s", backend)
	if cfg.Sandbox.Runtime != "" {
		fmt.Fprintf(&b, " (runtime: %s)", cfg.Sandbox.Runtime)
	}
	b.WriteString("\n\n")
	b.WriteString(sandbox.Report(ctx, priority))
	b.WriteString("\n\nChange with: /sandbox use <local|auto|docker|podman|wslc|container>")
	return SlashResult{Output: b.String()}
}

// cmdTheme switches the TUI color scheme live (P14.8; generalized beyond
// dark/light to any loaded theme by P16.7). Unlike /sandbox and /model,
// there's no daemon round trip: the theme is purely a TUI rendering concern,
// so the dispatcher only validates the name and hands off to the model via
// the same "\x00"-prefixed sentinel Output convention /humor and /sidebar
// use for local (non-server) UI state changes — the model is the one that
// actually rebinds the package-level colors, rebuilds m.th, and recreates
// the markdown renderer (see the slashResultMsg case in tui.go). Validation
// here checks d.workDir's project theme directory plus the user's and the
// embedded builtins (availableThemeNames) — a name valid here is guaranteed
// to load in tui.go's actual switch. This session only: set tui.theme in
// config to persist across restarts, same as the config-only /sandbox use.
func (d *SlashDispatcher) cmdTheme(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "\x00theme-show"}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	valid := availableThemeNames(d.workDir)
	found := false
	for _, v := range valid {
		if v == name {
			found = true
			break
		}
	}
	if !found {
		return SlashResult{Output: fmt.Sprintf("Unknown theme %q (want one of: %s).", args[0], strings.Join(valid, ", ")), IsError: true}
	}
	return SlashResult{Output: "\x00theme " + name}
}

// cmdNotify sets the P16.1 attention-system mode live, same pattern as
// cmdTheme: purely local TUI state, validated here and applied via the
// "\x00notify "-prefixed sentinel (see the slashResultMsg case in tui.go).
// This session only: set tui.notifications in config to persist.
func (d *SlashDispatcher) cmdNotify(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "\x00notify-show"}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	switch name {
	case "off", "bell", "desktop", "both":
		return SlashResult{Output: "\x00notify " + name}
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown notify mode %q (want off, bell, desktop, or both).", args[0]), IsError: true}
	}
}

// cmdArchive archives or unarchives the current session, or lists archived
// sessions. Archived sessions are hidden from normal listings but their data
// is preserved. Use /archive off to restore. To permanently remove a session,
// use `aegis sessions delete <id>`.
func (d *SlashDispatcher) cmdArchive(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		metas, err := d.client.ListArchivedSessions(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list archived sessions: %v", err), IsError: true}
		}
		var archived []api.SessionMeta
		for _, m := range metas {
			if m.Archived {
				archived = append(archived, m)
			}
		}
		if len(archived) == 0 {
			return SlashResult{Output: "No archived sessions."}
		}
		var b strings.Builder
		b.WriteString("Archived sessions:\n")
		for _, m := range archived {
			title := m.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "  %-8s  %-6s  %s  %s\n", m.ID[:8], m.Mode, m.UpdatedAt.Local().Format("2006-01-02 15:04"), title)
		}
		return SlashResult{Output: b.String()}
	}

	unarchive := len(args) > 0 && (strings.ToLower(args[0]) == "off" || strings.ToLower(args[0]) == "false")
	if unarchive {
		if err := d.client.UnarchiveSession(ctx, d.sessionID); err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to unarchive session: %v", err), IsError: true}
		}
		return SlashResult{Output: "Session unarchived. It will now appear in normal session listings."}
	}
	if err := d.client.ArchiveSession(ctx, d.sessionID); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to archive session: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Session %s archived.\nIt is now hidden from normal listings but all data is preserved.\nUse /archive off to restore, or `aegis sessions delete %s` to permanently remove.", d.sessionID[:8], d.sessionID[:8])}
}

// cmdPrune deletes non-archived sessions older than the given number of days
// (or the server's configured TTL if no argument is given) — same operation
// as `aegis sessions prune`.
func (d *SlashDispatcher) cmdPrune(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	days := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			return SlashResult{Output: fmt.Sprintf("Invalid days %q.", args[0]), IsError: true}
		}
		days = n
	}

	resp, err := d.client.PruneSessions(ctx, days)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Prune failed: %v", err), IsError: true}
	}
	if resp.Deleted == 0 {
		return SlashResult{Output: "No sessions matched (nothing pruned)."}
	}
	return SlashResult{Output: fmt.Sprintf("Pruned %d session(s).", resp.Deleted)}
}

// cmdBundle dispatches /bundle's two subcommands — info and install — same
// operations as `aegis bundle info`/`aegis bundle install`, run directly
// against the local filesystem (no daemon round trip, matching /sandbox and
// /security's local-computation convention).
func (d *SlashDispatcher) cmdBundle(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle [install|info <path-or-url>]", IsError: true}
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "info":
		return d.cmdBundleInfo(rest)
	case "install":
		return d.cmdBundleInstall(rest)
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown /bundle subcommand %q.\nUsage: /bundle [install|info <path-or-url>]", args[0]), IsError: true}
	}
}

// bundleIsGitURL mirrors internal/cli/bundle.go's isGitURL — kept as a
// separate unexported copy rather than shared, since internal/cli isn't (and
// shouldn't become) an import of internal/tui.
func bundleIsGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://") ||
		strings.HasPrefix(s, "ssh://")
}

// bundleResolveSource mirrors internal/cli/bundle.go's resolveBundle: a git
// URL is shallow-cloned into a temp dir (the caller must call cleanup once
// done reading from the returned path); a local path is returned as-is.
func bundleResolveSource(src string) (path string, cleanup func(), err error) {
	if !bundleIsGitURL(src) {
		return src, nil, nil
	}
	dir, err := os.MkdirTemp("", "aegis-bundle-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	rm := func() { os.RemoveAll(dir) }
	out, cloneErr := exec.Command("git", "clone", "--depth=1", src, dir).CombinedOutput()
	if cloneErr != nil {
		rm()
		return "", nil, fmt.Errorf("git clone %s: %v\n%s", src, cloneErr, strings.TrimSpace(string(out)))
	}
	return dir, rm, nil
}

// bundleScopeDir mirrors internal/cli/bundle.go's scopeDir: project scope is
// ./.aegis relative to the daemon's workspace, user scope is the configured
// data dir.
func bundleScopeDir(global bool) (string, error) {
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".aegis"), nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.DataDir, nil
}

func (d *SlashDispatcher) cmdBundleInfo(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle info <path-or-url>", IsError: true}
	}
	path, cleanup, err := bundleResolveSource(args[0])
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if cleanup != nil {
		defer cleanup()
	}
	b, err := bundle.Load(path)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s %s\n", b.Manifest.Name, b.Manifest.Version)
	if b.Manifest.Description != "" {
		fmt.Fprintf(&out, "%s\n", b.Manifest.Description)
	}
	if b.Manifest.Author != "" {
		fmt.Fprintf(&out, "by %s\n", b.Manifest.Author)
	}
	fmt.Fprintf(&out, "\n%d artifact(s):\n", len(b.Artifacts))
	for _, a := range b.Artifacts {
		fmt.Fprintf(&out, "  %s/%s\n", a.Kind, a.Rel)
	}
	if hash, err := b.ContentHash(); err == nil {
		fmt.Fprintf(&out, "\ncontent hash: %s\n(pin with `/bundle install %s global sha256:%s confirm` to detect upstream changes)\n", hash, args[0], strings.TrimPrefix(hash, "sha256:"))
	}
	return SlashResult{Output: out.String()}
}

// cmdBundleInstall mirrors `aegis bundle install`, adapted to the slash-command
// shape (no interactive flags): trailing tokens after the path may appear in
// any order — "global" installs into the user data dir instead of the
// project's .aegis/ (default), "sha256:<hash>" pins the P7.6 content-hash
// provenance check the same as the CLI's --expect-sha256, and "confirm" is
// required to actually write anything. Without "confirm" this only previews
// the manifest/artifacts/hash, same posture as /security install.
func (d *SlashDispatcher) cmdBundleInstall(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle install <path-or-url> [global] [sha256:<hash>] [confirm]", IsError: true}
	}
	src := args[0]
	global := false
	confirmed := false
	expectHash := ""
	for _, a := range args[1:] {
		switch {
		case strings.EqualFold(a, "global"):
			global = true
		case strings.EqualFold(a, "confirm"):
			confirmed = true
		case strings.HasPrefix(strings.ToLower(a), "sha256:"):
			expectHash = a
		default:
			expectHash = "sha256:" + a // allow the bare hex digest too
		}
	}

	path, cleanup, err := bundleResolveSource(src)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if cleanup != nil {
		defer cleanup()
	}
	b, err := bundle.Load(path)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	hash, hashErr := b.ContentHash()
	if expectHash != "" {
		if hashErr != nil {
			return SlashResult{Output: fmt.Sprintf("compute content hash: %v", hashErr), IsError: true}
		}
		if !strings.EqualFold(hash, expectHash) {
			return SlashResult{Output: fmt.Sprintf("Bundle content hash mismatch: got %s, expected %s — refusing to install (the source may have changed).", hash, expectHash), IsError: true}
		}
	}

	dest, err := bundleScopeDir(global)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	if !confirmed {
		scopeLabel := "project"
		if global {
			scopeLabel = "user (global)"
		}
		var preview strings.Builder
		fmt.Fprintf(&preview, "%s %s — %d artifact(s), installing into %s scope (%s):\n", b.Manifest.Name, b.Manifest.Version, len(b.Artifacts), scopeLabel, dest)
		for _, a := range b.Artifacts {
			fmt.Fprintf(&preview, "  %s/%s\n", a.Kind, a.Rel)
		}
		if hashErr == nil {
			fmt.Fprintf(&preview, "\ncontent hash: %s\n", hash)
		}
		scopeArg := ""
		if global {
			scopeArg = " global"
		}
		fmt.Fprintf(&preview, "\nRun `/bundle install %s%s confirm` to proceed.", src, scopeArg)
		return SlashResult{Output: preview.String()}
	}

	written, err := b.Install(dest, false)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if len(written) == 0 {
		return SlashResult{Output: "Nothing installed (all artifacts already exist)."}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Installed %q (%d file(s)) into %s:\n", b.Manifest.Name, len(written), dest)
	for _, w := range written {
		fmt.Fprintf(&out, "  %s\n", w)
	}
	skipped := len(b.Artifacts) - len(written)
	if skipped > 0 {
		fmt.Fprintf(&out, "(%d already present, skipped)\n", skipped)
	}
	if hashErr == nil {
		fmt.Fprintf(&out, "content hash: %s\n", hash)
	}
	return SlashResult{Output: out.String()}
}
