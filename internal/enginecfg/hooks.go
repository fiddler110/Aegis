package enginecfg

import (
	"log/slog"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/hooks"
)

// ExecHooks builds the user-configured lifecycle hooks (`hooks:` config, P4.4),
// or nil when none are configured. The concrete type is returned because the
// daemon also drives its session/sub-agent lifecycle events; pass the result
// through EngineHooks to get a gate-stack-safe engine.Hooks.
//
// P66.13/ARCH-06: `aegis chat` ran with no hooks at all, so a PreToolUse hook an
// operator configured to veto a tool call was silently absent on the scripted
// path — the same class of bypass as the bare permission gate next to it.
func ExecHooks(cfg *config.Config, logger *slog.Logger) *hooks.Exec {
	if cfg == nil || len(cfg.Hooks) == 0 {
		return nil
	}
	specs := make([]hooks.ExecSpec, 0, len(cfg.Hooks))
	for _, h := range cfg.Hooks {
		specs = append(specs, hooks.ExecSpec{
			Event:      h.Event,
			Command:    h.Command,
			Tools:      h.Tools,
			TimeoutSec: h.TimeoutSec,
		})
	}
	return hooks.NewExec(specs, logger)
}

// EngineHooks converts an exec-hook set into the engine's hook interface,
// returning a nil *interface* rather than a typed nil pointer when there is
// nothing configured.
//
// The distinction is the whole reason this exists: hooks.NewExec reports
// "nothing configured" with a nil *Exec, and assigning that straight into an
// engine.Hooks field yields a non-nil interface holding a nil pointer, which the
// engine would then happily call.
func EngineHooks(ex *hooks.Exec) engine.Hooks {
	if ex == nil {
		return nil
	}
	return ex
}
