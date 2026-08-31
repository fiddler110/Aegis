// Sandbox backend selection and the unsandboxed-auto-exec refusal it feeds.
// Extracted from server.go (L4). SelectSandbox is exported because every
// entry point that builds an execution surface must make this same choice.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// SelectSandbox picks the command-execution backend per cfg.Backend:
// "container" (the default) forces a runtime (or auto-detects one); "auto"
// detects and picks the best available; "os" uses OS-level isolation without
// a container runtime; "" or "local" runs commands directly on the host by
// design.
//
// "container"/"auto" cascade on failure: no container runtime falls back to
// the "os" backend (seatbelt/bwrap) before giving up to unsandboxed local,
// so a host without Docker/Podman running still gets OS-level isolation
// where available, rather than silently losing it just because container is
// now the default. cfg.Strict opts out of the whole cascade, not just the
// last step — it means "I asked for container, tell me if it isn't there"
// rather than "quietly substitute something else".
//
// A fallback to the unsandboxed local backend is a silent security downgrade
// for an operator who believes sandboxing is active (P7.4): it is always
// logged, reported back via the fallback/reason return values (surfaced by
// the caller via /healthz for clients to warn the user), and — when
// cfg.Strict is set — turned into a hard error instead of a silent fallback.
//
// cfg.Backend is expected to already be validated/aliased by
// config.SandboxConfig.Normalize (config.Load calls it, so any cfg built by
// the normal config path is covered); an unrecognized value reaching here
// anyway is rejected rather than silently treated as "local" (P25.2) — a
// container-runtime name typed straight into sandbox.backend (e.g. "podman"
// instead of backend: container + runtime: podman) used to fall through to
// this same default case and run every command unsandboxed on the host with
// no warning at all.
//
// Exported (not just server-internal) so the subprocess swarm worker
// (internal/cli/worker.go) can reconstruct the same sandbox backend the
// daemon selected instead of running its shell tool unsandboxed (P10.2).
func SelectSandbox(cfg config.SandboxConfig, cwd string, logger *slog.Logger) (sb sandbox.Backend, fallback bool, reason string, err error) {
	switch cfg.Backend {
	case "container", "auto":
		opts := sandbox.ContainerOpts{
			Image:      cfg.Image,
			Network:    cfg.Network,
			Priority:   sandbox.ParseRuntimes(cfg.Priority),
			Limits:     cfg.Limits.Sandbox(),
			Persistent: cfg.Persistent,
			SessionTTL: cfg.SandboxSessionTTL(),
			Logger:     logger,
		}
		// Only "container" honors an explicit forced runtime; "auto" always detects.
		if cfg.Backend == "container" {
			opts.Prefer = sandbox.ContainerRuntime(cfg.Runtime)
		}
		csb, cerr := sandbox.NewContainerBackend(opts)
		if cerr != nil {
			if cfg.Strict {
				return nil, false, "", fmt.Errorf("sandbox: no container runtime available for backend %q and sandbox.strict is set: %w", cfg.Backend, cerr)
			}
			// Cascade to OS-level isolation before giving up to unsandboxed
			// local: container is the default, but most hosts don't have
			// Docker/Podman running, and falling straight to local would be a
			// silent security downgrade for every macOS/Linux box that has
			// seatbelt/bwrap available. Mirrors the "os" case below.
			osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths)
			if oerr == nil {
				logger.Warn("sandbox: no container runtime available, falling back to OS-level sandboxing",
					"backend", cfg.Backend, "mechanism", osb.Name(), "err", cerr)
				reason = fmt.Sprintf("configured sandbox backend %q unavailable (%v) — falling back to OS-level sandboxing (%s)", cfg.Backend, cerr, osb.Name())
				return osb, true, reason, nil
			}
			logger.Warn("sandbox: no container runtime available and OS sandbox unavailable, falling back to local",
				"backend", cfg.Backend, "container_err", cerr, "os_err", oerr)
			reason = fmt.Sprintf("configured sandbox backend %q unavailable (%v); OS sandbox also unavailable (%v) — running unsandboxed on the host", cfg.Backend, cerr, oerr)
			return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), true, reason, nil
		}
		logger.Info("sandbox backend", "runtime", csb.DetectedRuntime(), "image", cfg.Image)
		// FIND-06 / P24.10: Docker/Podman socket access is privilege-equivalent
		// to local root regardless of the per-container hardening flags below —
		// surface that once per daemon start so an operator relying on the
		// container backend for isolation sees it. See docs/security_scan.md.
		if notice := sandbox.SocketPrivilegeNotice(csb.DetectedRuntime()); notice != "" {
			logger.Info(notice)
		}
		// P60.1: a configured cap that this runtime's CLI cannot express is
		// worse than no cap, because the operator believes it is in force. Say
		// so at selection time rather than leaving them to infer a bound from
		// the config file.
		// P60.2: a persistent container owns state, which means it also owns the
		// obligation to say so — both that commands now share an environment
		// (the thing that makes the backend usable) and that a runtime without
		// the verified detach/exec surface silently keeps the old per-command
		// behavior. Reap first: a daemon that crashed mid-session left a
		// container that will not expire for hours.
		if cfg.Persistent {
			if sandbox.SupportsPersistentContainer(csb.DetectedRuntime()) {
				logger.Info("sandbox: persistent session container enabled — state persists between tool calls",
					"runtime", csb.DetectedRuntime(), "ttl", cfg.SandboxSessionTTL())
				reapCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if n, rerr := sandbox.ReapOrphanSandboxes(reapCtx, csb.DetectedRuntime(), logger); rerr != nil {
					logger.Warn("sandbox: could not scan for orphaned containers", "err", rerr)
				} else if n > 0 {
					logger.Info("sandbox: reaped orphaned containers", "count", n)
				}
				cancel()
			} else {
				logger.Info("sandbox: sandbox.persistent has no effect on this runtime — one container per command, so no state survives a tool call",
					"runtime", csb.DetectedRuntime())
			}
		}
		if lim := cfg.Limits.Sandbox(); !lim.Empty() {
			if sandbox.SupportsResourceLimits(csb.DetectedRuntime()) {
				logger.Info("sandbox resource limits", "runtime", csb.DetectedRuntime(),
					"memory", lim.Memory, "cpus", lim.CPUs, "pids", lim.PIDs)
			} else {
				logger.Warn("sandbox: configured sandbox.limits are NOT enforced on this runtime — its CLI does not accept resource flags; a runaway command inside the sandbox can still consume the host",
					"runtime", csb.DetectedRuntime())
			}
		}
		return csb, false, "", nil
	case "os":
		// OS-level isolation without a container runtime (P4.7): seatbelt on
		// macOS, bwrap on Linux. Falls back to local when unavailable.
		osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths)
		if oerr != nil {
			if cfg.Strict {
				return nil, false, "", fmt.Errorf("sandbox: OS sandbox unavailable and sandbox.strict is set: %w", oerr)
			}
			logger.Warn("sandbox: OS sandbox unavailable, falling back to local", "err", oerr)
			reason = fmt.Sprintf("configured sandbox backend \"os\" unavailable (%v) — running unsandboxed on the host", oerr)
			return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), true, reason, nil
		}
		logger.Info("sandbox backend", "mechanism", osb.Name(), "network", cfg.Network)
		return osb, false, "", nil
	case "", "local":
		return sandbox.NewLocalBackendWithEnv(cfg.StripEnv), false, "", nil
	default:
		return nil, false, "", fmt.Errorf(
			"sandbox: unknown sandbox.backend %q (want \"local\", \"container\", \"auto\", or \"os\"); "+
				"if this names a container runtime, set sandbox.backend: container and sandbox.runtime: %q instead — "+
				"config.Load() normally rejects this before startup, so this cfg was built outside the normal config path",
			cfg.Backend, cfg.Backend,
		)
	}
}

// unsandboxedAutoExecError returns a startup-refusal error when
// permission.auto_approve_exec is set while the effective sandbox backend is
// the unsandboxed local one (P25.2), unless the operator has explicitly
// opted out via permission.allow_unsandboxed_auto_exec. Callers only invoke
// this once they've already established the backend is local (a
// *sandbox.LocalBackend) — this function itself doesn't re-check that, so
// it's a plain function of config rather than needing a sandbox.Backend,
// which keeps it unit-testable without spinning up a real backend.
func unsandboxedAutoExecError(perm config.PermissionConfig, configuredBackend string, sandboxFallback bool, sandboxFallbackReason string) error {
	// P66.1/SEC-09: `permission.mode: auto` reaches the same place
	// auto_approve_exec does — permission.Policy.Decide returns Allow for
	// CapExecute with no prompt — so keying the refusal on auto_approve_exec
	// alone left an equivalent unattended-RCE configuration behind a WARN.
	// That gap was the payload step of SEC-01's chain: it is what let a
	// project-supplied `mode: auto` be enough on its own.
	var setting string
	switch {
	case perm.AutoApproveExec:
		setting = "permission.auto_approve_exec is enabled"
	case perm.Mode == string(permission.ModeAuto):
		setting = "permission.mode is \"auto\""
	default:
		return nil
	}
	if perm.AllowUnsandboxedAutoExec {
		return nil
	}
	why := fmt.Sprintf("sandbox.backend is %q", configuredBackend)
	if sandboxFallback {
		why = sandboxFallbackReason
	}
	return fmt.Errorf(
		"refusing to start: %s but the effective sandbox backend is unsandboxed local execution (%s) — every model-issued shell command would run on the host with no approval and no isolation. "+
			"Configure a real sandbox (sandbox.backend: container or os), or set permission.allow_unsandboxed_auto_exec: true if this is intentional",
		setting, why,
	)
}
