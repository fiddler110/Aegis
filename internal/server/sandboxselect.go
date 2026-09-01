// Sandbox backend selection and the unsandboxed-auto-exec refusal it feeds.
// Extracted from server.go (L4). SelectSandbox is exported because every
// entry point that builds an execution surface must make this same choice.
package server

import (
	"context"
	"fmt"
	"log/slog"
	goruntime "runtime"
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
// now the default.
//
// cfg.Strict (P81.22/FIND-22: on by default) guards only the *last* step of
// that cascade — landing on unsandboxed local — not the cascade itself: it
// means "give me real isolation or tell me you can't", not "refuse anything
// but the exact runtime I named". A container→os cascade under strict still
// succeeds, because OS-level isolation is a real (if different) containment
// mechanism, not a downgrade to none; only a cascade that bottoms out at
// local — no container runtime and no OS sandbox mechanism, which is every
// current Windows box (P77.6) plus a macOS/Linux box missing both Docker and
// seatbelt/bwrap — is refused under strict. Earlier versions of this
// function had strict opt out of the whole cascade; that was fine when
// strict was an opt-in for someone who specifically wanted zero
// substitution, but flipping it to the default with that semantics intact
// would hard-fail the daemon on every macOS/Linux box that merely doesn't
// have Docker *running* right now — a far larger population than the one
// this posture exists to protect.
//
// A fallback to the unsandboxed local backend is a silent security downgrade
// for an operator who believes sandboxing is active (P7.4): it is always
// logged, reported back via the fallback/reason return values (surfaced by
// the caller via the authenticated /status for clients to warn the user), and — when
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
			Image:          cfg.Image,
			Network:        cfg.Network,
			Priority:       sandbox.ParseRuntimes(cfg.Priority),
			Limits:         cfg.Limits.Sandbox(),
			Persistent:     cfg.Persistent,
			SessionTTL:     cfg.SandboxSessionTTL(),
			EnvAllow:       cfg.EnvAllow,
			Logger:         logger,
			SecretExcludes: cfg.SecretExcludePaths,
		}
		// Only "container" honors an explicit forced runtime; "auto" always detects.
		if cfg.Backend == "container" {
			opts.Prefer = sandbox.ContainerRuntime(cfg.Runtime)
		}
		csb, cerr := sandbox.NewContainerBackend(opts)
		if cerr != nil {
			// Cascade to OS-level isolation regardless of cfg.Strict: strict
			// guards against landing on *no* isolation, not against landing on
			// a different (but still real) isolation mechanism than the one
			// named. See the doc comment above for why this changed.
			osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths)
			if oerr == nil {
				osb.WithEnvAllow(cfg.EnvAllow)
				logger.Warn("sandbox: no container runtime available, falling back to OS-level sandboxing",
					"backend", cfg.Backend, "mechanism", osb.Name(), "err", cerr)
				reason = fmt.Sprintf("configured sandbox backend %q unavailable (%v) — falling back to OS-level sandboxing (%s)", cfg.Backend, cerr, osb.Name())
				return osb, true, reason, nil
			}
			if cfg.Strict {
				return nil, false, "", strictUnavailableErr(cfg.Backend, cerr, oerr)
			}
			logger.Warn("sandbox: no container runtime available and OS sandbox unavailable, falling back to local",
				"backend", cfg.Backend, "container_err", cerr, "os_err", oerr)
			reason = fmt.Sprintf("configured sandbox backend %q unavailable (%v); OS sandbox also unavailable (%v) — running unsandboxed on the host", cfg.Backend, cerr, oerr)
			lb := sandbox.NewLocalBackendWithEnv(cfg.StripEnv).WithEnvAllow(cfg.EnvAllow).WithLimits(cfg.Limits.Sandbox())
			return lb, true, reason, nil
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
		// macOS, bwrap on Linux. Falls back to local when unavailable — a hard
		// failure under strict, since local is the "no isolation at all" case
		// strict exists to catch (there is nothing left to cascade to here).
		osb, oerr := sandbox.NewOSBackend(cwd, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths)
		if oerr != nil {
			if cfg.Strict {
				return nil, false, "", strictUnavailableErr(cfg.Backend, oerr, nil)
			}
			logger.Warn("sandbox: OS sandbox unavailable, falling back to local", "err", oerr)
			reason = fmt.Sprintf("configured sandbox backend \"os\" unavailable (%v) — running unsandboxed on the host", oerr)
			lb := sandbox.NewLocalBackendWithEnv(cfg.StripEnv).WithEnvAllow(cfg.EnvAllow).WithLimits(cfg.Limits.Sandbox())
			return lb, true, reason, nil
		}
		osb.WithEnvAllow(cfg.EnvAllow)
		logger.Info("sandbox backend", "mechanism", osb.Name(), "network", cfg.Network)
		return osb, false, "", nil
	case "", "local":
		lb := sandbox.NewLocalBackendWithEnv(cfg.StripEnv).WithEnvAllow(cfg.EnvAllow).WithLimits(cfg.Limits.Sandbox())
		if !cfg.Limits.Sandbox().Empty() && !sandbox.ResourceLimiterSupported() {
			logger.Warn("sandbox: configured sandbox.limits are NOT enforced on the local backend on this platform — resource caps are currently implemented via Windows job objects only; a runaway command can still consume the host (POSIX rlimit/cgroup support is a follow-up, see P81.22)")
		}
		return lb, false, "", nil
	default:
		return nil, false, "", fmt.Errorf(
			"sandbox: unknown sandbox.backend %q (want \"local\", \"container\", \"auto\", or \"os\"); "+
				"if this names a container runtime, set sandbox.backend: container and sandbox.runtime: %q instead — "+
				"config.Load() normally rejects this before startup, so this cfg was built outside the normal config path",
			cfg.Backend, cfg.Backend,
		)
	}
}

// strictUnavailableErr builds the sandbox.strict startup-refusal error for
// the case neither a container runtime nor OS-level isolation is available
// (P81.22/FIND-22) — the "unavailable isolation" case strict exists to catch,
// now hit by default on every host that used to silently land on unsandboxed
// local behind a startup WARN. osErr is nil when backend "os" was the one
// that failed directly (no separate container attempt to report).
//
// The message is actionable rather than a bare wrapped error because this is
// the exact failure the maintainer's own Windows dev box hits with the new
// default (P77.6: no OS-level sandbox backend on Windows yet) — a strict
// mode that fails closed with no next step would just be a worse warning.
func strictUnavailableErr(configuredBackend string, containerOrOSErr, osErr error) error {
	var detail string
	if osErr != nil {
		detail = fmt.Sprintf("container runtime unavailable (%v); OS-level sandbox also unavailable (%v)", containerOrOSErr, osErr)
	} else {
		detail = fmt.Sprintf("OS-level sandbox unavailable (%v)", containerOrOSErr)
	}
	hint := "install Docker or Podman (sandbox.backend: container), or bubblewrap/seatbelt for OS-level isolation (sandbox.backend: os)"
	if goruntime.GOOS == "windows" {
		hint = "install Docker Desktop or Podman for sandbox.backend: container — Windows has no OS-level sandbox backend yet (P77.6)"
	}
	return fmt.Errorf(
		"refusing to start: sandbox.strict is set (the default as of P81.22) and no real command-execution isolation is available for sandbox.backend %q — %s. "+
			"%s. If you understand the risk and want the old behavior (every model-issued shell command runs directly on the host, unconfined), "+
			"set sandbox.strict: false, or sandbox.backend: local to choose that outright without the warning",
		configuredBackend, detail, hint,
	)
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

// cronAutoApproveGuard refuses cron_create's auto_approve on an unsandboxed
// local backend (P81.23/FIND-23), mirroring unsandboxedAutoExecError's own
// startup refusal and its allow_unsandboxed_auto_exec escape hatch — a cron
// job fires with nobody present to approve anything, exactly like
// auto_approve_exec/mode:auto, so it is refused the same way rather than
// getting its own weaker rule. A plain method value (wired via
// cron.Scheduler.SetAutoApproveGuard), so it reads s.cfg/s.sandbox fresh on
// every Create call rather than whatever they were when the daemon started.
func (s *Server) cronAutoApproveGuard() error {
	if _, isLocal := s.sandbox.(*sandbox.LocalBackend); !isLocal {
		return nil
	}
	if s.cfg.Permission.AllowUnsandboxedAutoExec {
		return nil
	}
	why := fmt.Sprintf("sandbox.backend is %q", s.cfg.Sandbox.Backend)
	if s.sandboxFallback {
		why = s.sandboxFallbackReason
	}
	return fmt.Errorf(
		"cron: refusing to create an auto_approve job — the effective sandbox backend is unsandboxed local execution (%s). "+
			"This job would fire unattended, with no approval and no isolation. Configure a real sandbox "+
			"(sandbox.backend: container or os), or set permission.allow_unsandboxed_auto_exec: true if this is intentional",
		why,
	)
}
