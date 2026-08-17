package enginecfg

import (
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/fiddler110/aegis/internal/toolpath"
)

// BuiltinOptions fills the config-derived half of builtin.Options for a run
// rooted at root.
//
// builtin.Options is a 27-field struct filled differently at all five call sites
// (QUAL-06). Most of those fields are *host* wiring — a sandbox backend, a task
// manager, an LSP manager — which genuinely differ between the daemon and a
// one-shot CLI process, and the daemon is right to be the only site that passes
// them. What must not differ is the half that is a straight reading of config,
// and that half had: the CLI paths all omitted Commands, so the entire
// toolpath/ripgrep contract was inert there — `grep` silently fell back to the
// pure-Go walker on the path a scripted run uses, ignoring the operator's
// `commands:` config.
//
// LocalProfile is decided here for every caller that uses it, which is what
// TestEveryRegisterCallSiteDecidesTheLocalProfile (P62.10) asks each site to
// have thought about: reading provider.LocalPromptProfile() once here is the
// decision, made in one place instead of five.
//
// Callers overlay the host wiring on the returned value:
//
//	opts := enginecfg.BuiltinOptions(cfg, cwd)
//	opts.Sandbox = workerSandbox
//	builtin.Register(reg, opts)
//
// BuiltinSkills is filled from `skills.builtin_enabled`; a caller that layers
// more on for one run (`aegis chat --skill`) overwrites it the same way.

func BuiltinOptions(cfg *config.Config, root string) builtin.Options {
	if cfg == nil {
		return builtin.Options{Root: root}
	}
	return builtin.Options{
		Root:    root,
		DataDir: cfg.DataDir,
		// P66.13/QUAL-06: the one field whose absence was a capability loss
		// rather than a difference. toolpath.New honors the user's `commands:`
		// config (and, when the workspace is untrusted, the frozen values
		// config.Load already substituted), so passing it is what makes
		// ripgrep/git/gh reachable at all.
		Commands:                toolpath.New(cfg.Commands),
		KrokiURL:                cfg.Diagram.KrokiURL,
		BuiltinSkills:           cfg.Skills.BuiltinEnabled,
		SecurityScan:            security.OptionsFromConfig(cfg.Security),
		DASTAllowedTargets:      cfg.Security.DAST.AllowedTargets,
		DASTAllowActive:         cfg.Security.DAST.AllowActive,
		LocalProfile:            cfg.Provider.LocalPromptProfile(),
		ToolFamilies:            cfg.Tools.Families,
		GitPreCommitTestCommand: cfg.Git.PreCommitTestCommand,
		GitPreCommitTestTimeout: time.Duration(cfg.Git.PreCommitTestTimeoutSec) * time.Second,
	}
}
