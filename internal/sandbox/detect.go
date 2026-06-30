package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
)

// RuntimeInfo reports the availability of one container runtime on the host.
type RuntimeInfo struct {
	Runtime   ContainerRuntime
	Available bool
	Version   string // human-readable version line; empty when unavailable
	Detail    string // note on why it is unavailable, or an install hint
}

// probeFunc reports a runtime's availability. It is a seam so tests can supply
// canned results without any runtime installed.
type probeFunc func(ctx context.Context, rt ContainerRuntime) RuntimeInfo

// probeTimeout bounds each runtime probe so a hung or absent daemon (e.g.
// `docker info` with the engine stopped) cannot block detection.
const probeTimeout = 3 * time.Second

// probeRuntime is the package-level probe used by the exported detectors.
var probeRuntime probeFunc = realProbe

// candidateRuntimes returns the runtimes worth probing on a given OS, in the
// default preference order. WSL containers are preferred on Windows.
func candidateRuntimes(goos string) []ContainerRuntime {
	switch goos {
	case "windows":
		return []ContainerRuntime{RuntimeWSL, RuntimeDocker, RuntimePodman}
	case "darwin":
		return []ContainerRuntime{RuntimeDocker, RuntimePodman, RuntimeAppleContainers}
	default:
		return []ContainerRuntime{RuntimeDocker, RuntimePodman}
	}
}

// DefaultPriority returns the OS-default auto-selection order for this host.
func DefaultPriority() []ContainerRuntime { return candidateRuntimes(runtime.GOOS) }

// ParseRuntimes converts config runtime names into ContainerRuntime values,
// skipping blanks. "wsl" is accepted as an alias for "wslc".
func ParseRuntimes(names []string) []ContainerRuntime {
	out := make([]ContainerRuntime, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if n == "wsl" {
			n = string(RuntimeWSL)
		}
		out = append(out, ContainerRuntime(n))
	}
	return out
}

// DetectRuntimes probes every candidate runtime for the host OS and returns
// their availability, in preference order.
func DetectRuntimes(ctx context.Context) []RuntimeInfo {
	return detectRuntimes(ctx, runtime.GOOS, probeRuntime)
}

// DetectBest returns the first available runtime in priority order. An empty
// priority falls back to the OS default order.
func DetectBest(ctx context.Context, priority []ContainerRuntime) (ContainerRuntime, bool) {
	return detectBest(ctx, runtime.GOOS, priority, probeRuntime)
}

func detectRuntimes(ctx context.Context, goos string, probe probeFunc) []RuntimeInfo {
	cands := candidateRuntimes(goos)
	out := make([]RuntimeInfo, 0, len(cands))
	for _, rt := range cands {
		out = append(out, probe(ctx, rt))
	}
	return out
}

func detectBest(ctx context.Context, goos string, priority []ContainerRuntime, probe probeFunc) (ContainerRuntime, bool) {
	order := priority
	if len(order) == 0 {
		order = candidateRuntimes(goos)
	}
	for _, rt := range order {
		if probe(ctx, rt).Available {
			return rt, true
		}
	}
	return "", false
}

// bestFromInfos picks the first available runtime in priority order from
// already-probed infos, avoiding a second round of probes. An empty priority
// uses the order in which infos were collected (the OS default order).
func bestFromInfos(infos []RuntimeInfo, priority []ContainerRuntime) (ContainerRuntime, bool) {
	avail := make(map[ContainerRuntime]bool, len(infos))
	for _, in := range infos {
		avail[in.Runtime] = in.Available
	}
	order := priority
	if len(order) == 0 {
		order = make([]ContainerRuntime, len(infos))
		for i, in := range infos {
			order[i] = in.Runtime
		}
	}
	for _, rt := range order {
		if avail[rt] {
			return rt, true
		}
	}
	return "", false
}

// Report probes runtimes and returns a human-readable table marking the one
// that `auto` selection would pick (given priority). Shared by the CLI
// (`aegis sandbox detect`) and the TUI (`/sandbox`).
func Report(ctx context.Context, priority []ContainerRuntime) string {
	infos := DetectRuntimes(ctx)
	best, hasBest := bestFromInfos(infos, priority)

	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUNTIME\tAVAILABLE\tVERSION / DETAIL\tAUTO")
	for _, in := range infos {
		avail := "no"
		if in.Available {
			avail = "yes"
		}
		detail := in.Version
		if detail == "" {
			detail = in.Detail
		}
		marker := ""
		if hasBest && in.Runtime == best {
			marker = "←"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", in.Runtime, avail, detail, marker)
	}
	tw.Flush()
	if !hasBest {
		b.WriteString("\nNo container runtime available — `auto`/`container` backends fall back to local execution.")
	}
	return b.String()
}

// realProbe checks a runtime's CLI on PATH and runs a quick version/info probe
// under probeTimeout.
func realProbe(ctx context.Context, rt ContainerRuntime) RuntimeInfo {
	info := RuntimeInfo{Runtime: rt}
	bin, args, hint := probeSpec(rt)
	if bin == "" {
		info.Detail = "unsupported on this OS"
		return info
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		// WSL exposes both `wslc` and the `container` alias; the alias name
		// collides with Apple Containers but never on the same OS.
		if rt == RuntimeWSL {
			if p2, err2 := exec.LookPath("container"); err2 == nil {
				path, bin = p2, "container"
			}
		}
		if path == "" {
			info.Detail = bin + " not found on PATH"
			if hint != "" {
				info.Detail += "; " + hint
			}
			return info
		}
	}

	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(pctx, path, args...).CombinedOutput()
	if err != nil {
		info.Detail = "installed but not ready: " + firstLine(string(out))
		if hint != "" {
			info.Detail += "; " + hint
		}
		return info
	}
	info.Available = true
	info.Version = firstLine(string(out))
	return info
}

// probeSpec returns the binary, probe args, and install hint for a runtime, or
// an empty binary when the runtime is not applicable to the current OS.
func probeSpec(rt ContainerRuntime) (bin string, args []string, hint string) {
	switch rt {
	case RuntimeDocker:
		return "docker", []string{"version", "--format", "{{.Server.Version}}"}, ""
	case RuntimePodman:
		return "podman", []string{"version", "--format", "{{.Version}}"}, ""
	case RuntimeWSL:
		if runtime.GOOS != "windows" {
			return "", nil, ""
		}
		return "wslc", []string{"--version"}, "install with: wsl --update --pre-release (requires WSL >= 2.9.3)"
	case RuntimeAppleContainers:
		if runtime.GOOS != "darwin" {
			return "", nil, ""
		}
		return "container", []string{"--version"}, ""
	default:
		return "", nil, ""
	}
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(first)
}
