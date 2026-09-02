package config

// TUIConfig holds terminal UI preferences.
type TUIConfig struct {
	// HumorMode enables D&D-themed thinking phrases while the model generates.
	// Set to false for plain "thinking…" / "working…" status text.
	HumorMode bool `koanf:"humor_mode"`
	// Theme selects the TUI color scheme: "auto" (default — detect the
	// terminal's light/dark background at startup, P40.5), "dark", "light", an
	// embedded builtin (catppuccin, dracula, gruvbox, tokyonight), or a
	// custom name loaded from .aegis/themes/<name>.json (project) or
	// ~/.aegis/themes/<name>.json (user) — see internal/tui/theme_loader.go.
	Theme string `koanf:"theme"`
	// Notifications controls the P16.1 attention system fired on stream-end,
	// approval-pending, and error while the terminal isn't focused: "off",
	// "bell", "desktop", or "both" (default).
	Notifications string `koanf:"notifications"`
	// ImageRendering controls the P16.9 inline thumbnail shown in the
	// transcript when an image is attached: "auto" (default — the kitty
	// graphics protocol when the terminal answers P67.9's capability query
	// saying it speaks it, else a truecolor half-block thumbnail when the
	// detected color profile supports it), "off", "halfblock" (force the
	// half-block tier), or "kitty" (P40.4 — force the graphics protocol on a
	// terminal that supports it but answers no queries). The probe itself has
	// its own escape hatch, AEGIS_TERM_CAPS (see internal/termcaps).
	ImageRendering string `koanf:"image_rendering"`
	// Keybindings remaps named TUI actions (P13.3.5). Keys are the binding
	// names from internal/tui's keyMap (e.g. "terminal", "palette",
	// "diagnose" — lowercased struct field name), values are one or more
	// key sequences in bubbles/key form (e.g. "ctrl+x", "alt+t"). Unlisted
	// actions keep their hardcoded default. Unknown action names are
	// rejected at TUI startup.
	Keybindings map[string][]string `koanf:"keybindings"`
	// Mouse controls whether the TUI captures the mouse (P74.19): "on"
	// (default) or "off". Capturing the mouse is what makes Aegis's own
	// drag-selection possible, but it also stops the terminal emulator from
	// offering its own click-drag select and copy-on-select — the only
	// thing that reliably works today for a `tmux`/`kitty` copy-mode
	// workflow or (before P74.20's OSC 52 path) over SSH. Setting this to
	// "off" releases capture while keeping alt-screen, so resize re-wrap
	// still works — the one combination `/scrollback` can't give you, since
	// it releases both. The cost: no wheel scroll (a released wheel event
	// goes to the terminal emulator in alt-screen), no click-to-focus, and
	// `/copy`'s drag-selection goes idle. This is an escape hatch, not a
	// default — most people are better served by P74.20's OSC 52 clipboard
	// fix, which solves the SSH case without giving up capture.
	Mouse string `koanf:"mouse"`
	// ReducedMotion (P74.10) disables the continuous "working" animations —
	// the shimmer sweep across the status line, the streaming caret's blink,
	// the cycling thinking phrase, and the pending tool card's shimmer frame
	// — freezing each at its last frame instead. Off by default. This is
	// both an accessibility setting (the shimmer is a moving-luminance sweep,
	// the class of animation vestibular sensitivity reacts to) and a CPU one
	// (it skips the per-tick transcript re-render on a machine that may be
	// simultaneously running local inference).
	ReducedMotion bool `koanf:"reduced_motion"`
	// Dashboard configures the sidebar's section set/order.
	Dashboard DashboardConfig `koanf:"dashboard"`
}

// DashboardConfig controls which sections the TUI sidebar shows and in what
// order (P<dashboard>), mirroring Keybindings above: named identifiers,
// validated at TUI startup rather than silently ignored when misspelled.
type DashboardConfig struct {
	// Sections lists sidebar section identifiers in display order, e.g.
	// ["session", "mode", "cost"]. Empty (default) keeps the built-in fixed
	// order and full section set. Valid identifiers are documented alongside
	// internal/tui's validateDashboardSections; an unrecognized one is
	// rejected at startup, not silently dropped.
	Sections []string `koanf:"sections"`
}

// CleanupConfig controls automatic pruning of old sessions.
type CleanupConfig struct {
	// SessionTTLDays is how many days since last update before a non-archived
	// session is automatically deleted. 0 disables auto-cleanup.
	SessionTTLDays int `koanf:"session_ttl_days"`
	// ArchivedSessionTTLDays is how many days since a session was archived
	// before it is automatically deleted, using session.Store.PruneArchived
	// (P81.24). Archiving is how an operator says "keep this, out of the
	// way" — SessionTTLDays deliberately never touches an archived session —
	// but that made archiving the one gesture that opted a conversation, its
	// traces and its checkpoint file copies out of retention *permanently*.
	// 0 disables auto-cleanup of archived sessions, matching SessionTTLDays'
	// own default-off convention; an operator who wants "keep forever" gets
	// exactly that by leaving this unset.
	ArchivedSessionTTLDays int `koanf:"archived_session_ttl_days"`
	// CheckpointTTLDays is how many days a checkpoint (and the whole-file
	// snapshots it holds) is kept, independent of whether its owning session
	// still exists (P81.24) — checkpoint.Store.PruneOlderThan. A checkpoint's
	// value decays with wall-clock time, not with session lifetime: rewind
	// only ever reaches a session's recent turns, so a checkpoint made a
	// month ago is dead weight in the database whether or not its session
	// was ever deleted or archived. 0 disables auto-cleanup.
	CheckpointTTLDays int `koanf:"checkpoint_ttl_days"`
	// IntervalHours is how often the pruner runs. Defaults to 24. Shared by
	// SessionTTLDays, ArchivedSessionTTLDays and CheckpointTTLDays — they are
	// three retention horizons over the same one daemon-lifetime ticker, not
	// three separate schedules to configure.
	IntervalHours int `koanf:"interval_hours"`
}

// SwarmConfig configures multi-agent sub-agent execution.
type SwarmConfig struct {
	Backend string `koanf:"backend"` // "in_process" (default) or "subprocess"
}

// ToolsConfig tunes which optional built-in tool families are registered.
type ToolsConfig struct {
	// Families names deferred tool families to register that the active
	// prompt profile would otherwise omit (P62.6). Additive, and empty by
	// default, so an unset key is unambiguous — the same shape as
	// skills.builtin_enabled, and for the same reason.
	//
	// It exists because the local prompt profile drops the coordination,
	// scheduling and long-term-memory families ("team", "cron", "entity"):
	// thirteen of the twenty-six deferred tools, advertised on every turn of
	// every session, for capabilities a small-model file-scoped task does not
	// reach for. The default profile registers everything and ignores this
	// key. Naming a family here puts it back — a local model driving a swarm
	// is a real configuration, just not the one the profile is tuned for.
	//
	// See builtin.ToolFamilies for the recognized names; an unknown name is a
	// no-op rather than an error, since a family can be retired.
	Families []string `koanf:"families"`
}

// WorkspaceConfig widens what the session's workspace-confined tools can
// reach beyond the single directory Aegis was started in (P52.13).
type WorkspaceConfig struct {
	// AdditionalRoots names directories outside the session workdir that
	// workspace-confined tools may resolve paths into. This exists for the
	// cross-repo shape the single-root model makes inexpressible: read
	// research artifacts out of repo A, write the formal document into repo
	// B. The alternative — starting Aegis from a common parent — works but
	// widens confinement far past what the task needs and inflates the repo
	// map.
	//
	// Each root needs its own `aegis trust` decision; an additional root does
	// not inherit the primary workspace's trust (a trusted project must not
	// be able to nominate an untrusted directory and have it silently
	// honored). Untrusted entries are dropped with a warning rather than
	// failing the load.
	AdditionalRoots []AdditionalRoot `koanf:"additional_roots"`
}

// AdditionalRoot is one entry of workspace.additional_roots.
type AdditionalRoot struct {
	// Path is the directory to admit. A relative path resolves against the
	// session workdir, so `../research` in a project config means what it
	// looks like.
	Path string `koanf:"path"`
	// Writable admits writes as well as reads. Off by default — the common
	// case is reading source material out of the additional root, and
	// read-only is a cheap, meaningful restriction on a directory the model
	// was not started in.
	Writable bool `koanf:"writable"`
}

// OutputGuardConfig sets the default output-validation behaviour applied to
// every persona unless the persona overrides or disables it.
type OutputGuardConfig struct {
	Enabled    bool   `koanf:"enabled"`     // global default; per-session /guard toggles from this
	Mode       string `koanf:"mode"`        // "llm" (default) or "schema"
	Rubric     string `koanf:"rubric"`      // default llm rubric
	MaxRetries int    `koanf:"max_retries"` // corrective retries on failure

	// Model pins the model guard verdict calls run on, outranking every default
	// (P59.5). Empty means: provider.small_model on a cloud provider, the
	// session's own model on a local Ollama one — where naming a second model
	// evicts the resident one and pays a cold reload on the next turn, costing
	// far more than the cheaper verdict saves. Set this when you have the VRAM
	// to hold both and want the split anyway.
	Model string `koanf:"model"`
}

// PersonaOverride holds per-persona config overrides keyed by persona name.
type PersonaOverride struct {
	Model string `koanf:"model"` // "" = use global provider.model
}

// SkillsConfig controls which of the skills embedded in the Aegis binary
// (see `aegis skills list`) are active for this project/user.
type SkillsConfig struct {
	// BuiltinEnabled names which embedded built-in skills are active. Empty
	// by default: built-ins ship in the binary but stay dormant (no
	// system-prompt cost) until named here, via `aegis skills enable
	// <name>`, or the /skills TUI command. Project-local (.aegis/skills) and
	// user (~/.aegis/skills) skill files are unaffected by this list — those
	// are always active since a user chose to author them.
	BuiltinEnabled []string `koanf:"builtin_enabled"`
}

// DefaultGuardRubric is the generic quality rubric applied when output guarding
// is on and a persona declares no rubric of its own.
const DefaultGuardRubric = "The response must directly and completely address the request, " +
	"contain no unfinished work (TODO markers, \"left as an exercise\", stubbed-out logic), " +
	"and ground factual claims in tool output where applicable. Example or placeholder values " +
	"clearly used as such in documentation (e.g. an illustrative IP address, hostname, or " +
	"<your-api-key>-style token) are acceptable, especially when the real value depends on the " +
	"reader's own environment and was never supplied to the model."

// DiagramConfig configures diagram rendering.
type DiagramConfig struct {
	KrokiURL string `koanf:"kroki_url"` // Kroki endpoint for multi-format rendering
}

// LogConfig bounds the harness log file's growth (GAP-02). A missing/zero
// MaxSizeMB disables rotation entirely — logging.Options preserves today's
// unbounded-append behavior in that case — but the defaults below always
// supply a positive value, so rotation is on unless a user deliberately
// unsets it.
type LogConfig struct {
	// MaxSizeMB is the size aegis.log may reach before it is rotated to
	// aegis.log.1 and a fresh file is started. <= 0 disables rotation.
	MaxSizeMB int `koanf:"max_size_mb"`
	// MaxBackups is how many rotated files (aegis.log.1, .2, ...) are kept;
	// the oldest is deleted once this is exceeded.
	MaxBackups int `koanf:"max_backups"`
}
