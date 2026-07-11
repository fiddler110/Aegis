package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// WorkerSpec is the JSON contract between the SubprocessBackend and the headless
// worker process it launches. The parent writes it to a temp file; the worker
// reads it, runs the teammate to completion, and records the result in the
// mailbox under MailboxRoot.
type WorkerSpec struct {
	Identity    Identity    `json:"identity"`
	Config      SpawnConfig `json:"config"`
	MailboxRoot string      `json:"mailbox_root"`
	// SessionDBPath is the daemon's session SQLite file, passed through so the
	// worker can open its own connection to it and record file snapshots
	// against Config.CheckpointID (P9) — checkpoint capture otherwise misses
	// subprocess-mode sub-agents entirely, since the in-process Snapshotter
	// lives in a ctx.Value the worker's separate process can never see.
	// Empty when checkpointing is disabled or Config.CheckpointID is unset.
	SessionDBPath string `json:"session_db_path,omitempty"`

	// RemainingBudgetUSD / RemainingTokens (P10.3) are the parent's own
	// cost.BudgetUSD / cost.MaxTokensPerRun ceiling minus whatever the shared
	// fan-out tracker has already spent at spawn time, computed by
	// SubprocessBackend.Spawn from the ctx-carried tracker (D1/P10.5). A
	// worker uses these as its own engine budget instead of the daemon's full
	// configured cap — otherwise every subprocess teammate got a fresh full
	// allowance, so N concurrent/sequential spawns enforced N times the
	// intended ceiling instead of one shared one. Zero means "no cap
	// configured on the parent side" (the worker falls back to its own
	// cfg.Cost.* value, which is 0/unlimited in that case too — same result).
	// Once the shared cap is exhausted, a worker still gets fairShareFraction
	// of the *original* cap rather than a near-zero remainder (FIND-14/
	// P24.15) — an early expensive sibling can no longer starve a later one
	// down to essentially nothing.
	RemainingBudgetUSD float64 `json:"remaining_budget_usd,omitempty"`
	RemainingTokens    int     `json:"remaining_tokens,omitempty"`
}

// costTracker is the narrow slice of *cost.Tracker's API this package needs
// to compute and fold back subprocess-worker spend, kept local so swarm does
// not need to import internal/cost (mirrors the any-typed context value in
// WithCostTracker/CostTrackerFromContext).
type costTracker interface {
	TotalUSD() float64
	TotalTokens() int
	AddWorkerCost(costUSD float64, tokens int)
}

// minRemainingBudgetUSD/minRemainingTokens are the absolute last-resort floor
// passed to a worker when the parent's cap is already exhausted at spawn time
// *and* fairShareFraction (below) would itself round down to zero (e.g. a
// sub-cent budgetUSD): a literal 0 would be indistinguishable from "no cap
// configured" (which also serializes as 0/omitted), so a tiny positive floor
// keeps the override active — the worker's very first budget check still lets
// one turn through (a fresh tracker starts at 0, which is not >= floor)
// before tripping on that turn's actual spend, the same one-turn slack the
// in-process budget gate has.
const (
	minRemainingBudgetUSD = 0.000001
	minRemainingTokens    = 1
)

// fairShareFraction is FIND-14's guaranteed minimum slice of the *original*
// cap (not the shared tracker's dwindling remainder) handed to every spawned
// worker, regardless of how much siblings spawned earlier have already
// consumed. Without this, remainingBudget/remainingTokens degrade to the
// near-zero epsilon floor above as soon as the shared cap is exhausted, so
// one expensive/runaway early sub-agent could starve every later sibling of
// a swarm run down to essentially nothing — even though those siblings still
// have real work to do. SubprocessBackend has no notion of how many teammates
// a swarm run intends to spawn in total (SpawnConfig carries no team-size
// hint), so rather than trying to divide the cap by an unknown N, this uses a
// fixed conservative fraction: generous enough that a handful of siblings can
// each get a meaningful floor, small enough that it can't itself blow the
// budget by much even if every spawned worker ends up needing its floor
// (worst case total spend across floors alone is bounded at 5x the original
// cap, which is an acceptable trade against the fairness gap this closes).
const fairShareFraction = 0.2

func remainingBudget(cap, spent float64) float64 {
	if r := cap - spent; r > 0 {
		return r
	}
	if floor := cap * fairShareFraction; floor > minRemainingBudgetUSD {
		return floor
	}
	return minRemainingBudgetUSD
}

func remainingTokens(cap, spent int) int {
	if r := cap - spent; r > 0 {
		return r
	}
	if floor := int(float64(cap) * fairShareFraction); floor > minRemainingTokens {
		return floor
	}
	return minRemainingTokens
}

// SubprocessBackend runs each teammate as a separate headless process (the
// harness binary invoked with WorkerArgs), giving real OS-level isolation.
// Results are read back from the teammate's mailbox after the process exits.
type SubprocessBackend struct {
	exePath         string
	workerArgs      []string
	registry        *Registry
	mailboxRoot     string
	sessionDBPath   string // passed to workers for checkpoint capture (P9); "" disables it
	budgetUSD       float64
	maxTokensPerRun int
	onStop          func(Identity, Result)
	wg              sync.WaitGroup
}

// OnStop registers a teammate-completion listener (SUBAGENT_STOP).
func (b *SubprocessBackend) OnStop(fn func(Identity, Result)) { b.onStop = fn }

// NewSubprocessBackend builds a subprocess backend. exePath is the harness
// executable; workerCmd is the hidden worker subcommand (e.g. "__worker").
// sessionDBPath is the daemon's session SQLite file, threaded through to
// workers so their file writes can be captured against the parent turn's
// checkpoint (P9); pass "" to disable that (checkpointing not configured).
// budgetUSD/maxTokensPerRun are the daemon's configured cost.budget_usd/
// cost.max_tokens_per_run caps (0 = unlimited), used to compute each spawned
// worker's remaining allowance (P10.3); pass 0 for either to skip that cap.
func NewSubprocessBackend(exePath, workerCmd string, registry *Registry, mailboxRoot, sessionDBPath string, budgetUSD float64, maxTokensPerRun int) *SubprocessBackend {
	return &SubprocessBackend{
		exePath:         exePath,
		workerArgs:      []string{workerCmd},
		registry:        registry,
		mailboxRoot:     mailboxRoot,
		sessionDBPath:   sessionDBPath,
		budgetUSD:       budgetUSD,
		maxTokensPerRun: maxTokensPerRun,
	}
}

// Spawn launches a worker process for the teammate and returns a handle.
func (b *SubprocessBackend) Spawn(ctx context.Context, cfg SpawnConfig) (*Handle, error) {
	id := NewIdentity(cfg.Name, cfg.Team, cfg.ParentSessionID)
	b.registry.Add(id)

	spec := WorkerSpec{Identity: id, Config: cfg, MailboxRoot: b.mailboxRoot, SessionDBPath: b.sessionDBPath}
	// P10.3: hand the worker what's left of the fan-out tree's shared budget,
	// not the daemon's full configured cap — otherwise every subprocess
	// teammate enforces its own fresh ceiling, so N spawns allow N times the
	// intended spend. tracker is nil for a spawn whose context was severed
	// from the top-level request (e.g. some background paths), in which case
	// the worker just falls back to its own unmodified cfg.Cost.* values.
	if tracker, ok := CostTrackerFromContext(ctx).(costTracker); ok {
		if b.budgetUSD > 0 {
			spec.RemainingBudgetUSD = remainingBudget(b.budgetUSD, tracker.TotalUSD())
		}
		if b.maxTokensPerRun > 0 {
			spec.RemainingTokens = remainingTokens(b.maxTokensPerRun, tracker.TotalTokens())
		}
	}

	specPath, err := writeSpec(spec)
	if err != nil {
		b.registry.Update(id.AgentID, StatusFailed, "spec write failed")
		return nil, err
	}

	h := &Handle{Identity: id, done: make(chan Result, 1)}
	b.wg.Go(func() {
		defer os.Remove(specPath)
		res := b.runWorker(ctx, id, specPath)
		// Fold the worker's self-reported spend back into the shared
		// tracker so a sibling spawned after this one sees the updated
		// total when it computes its own remaining allowance above.
		if tracker, ok := CostTrackerFromContext(ctx).(costTracker); ok {
			tracker.AddWorkerCost(res.CostUSD, res.Tokens)
		}
		status := StatusDone
		if res.Failed() {
			status = StatusFailed
		}
		b.registry.Update(id.AgentID, status, summarize(res.Output, res.Err))
		if b.onStop != nil {
			b.onStop(id, res)
		}
		h.done <- res
	})
	return h, nil
}

// runWorker executes the worker process and reads its result from the mailbox.
func (b *SubprocessBackend) runWorker(ctx context.Context, id Identity, specPath string) Result {
	args := append(append([]string{}, b.workerArgs...), "--spec", specPath)
	cmd := exec.CommandContext(ctx, b.exePath, args...)
	cmd.Env = filteredEnv() // inherit only required environment variables
	// Binds the worker to an OS-level lifecycle guarantee (P9, platform-specific:
	// see procattr_*.go) so an abnormal daemon death doesn't orphan it running
	// indefinitely — sysProcAttr governs process-group/Pdeathsig at creation
	// time; afterStart handles what can only be done once the process exists
	// (Windows Job Object assignment).
	cmd.SysProcAttr = sysProcAttr()
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{AgentID: id.AgentID, Err: fmt.Sprintf("worker process failed to start: %v", err)}
	}
	afterStart(cmd)
	runErr := cmd.Wait()

	res := Result{AgentID: id.AgentID}
	if mb, e := OpenMailbox(b.mailboxRoot, id); e == nil {
		if msgs, _ := mb.ReadAll(false); len(msgs) > 0 {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Type != MsgResult {
					continue
				}
				res.Output = msgs[i].Text
				if errStr, ok := msgs[i].Payload["error"].(string); ok {
					res.Err = errStr
				}
				// JSON numbers decode as float64 into map[string]any.
				if v, ok := msgs[i].Payload["cost_usd"].(float64); ok {
					res.CostUSD = v
				}
				if v, ok := msgs[i].Payload["tokens"].(float64); ok {
					res.Tokens = int(v)
				}
				break
			}
		}
	}

	// If the process died without recording a result, synthesize a failure.
	if runErr != nil && res.Output == "" && res.Err == "" {
		res.Err = fmt.Sprintf("worker process failed: %v: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	return res
}

// Shutdown waits for all worker processes to finish or ctx to cancel.
func (b *SubprocessBackend) Shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// writeSpec serializes a WorkerSpec to a temp file and returns its path.
func writeSpec(spec WorkerSpec) (string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("swarm: marshal worker spec: %w", err)
	}
	f, err := os.CreateTemp("", "aegis-worker-*.json")
	if err != nil {
		return "", fmt.Errorf("swarm: create spec file: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("swarm: chmod spec file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("swarm: write spec: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// maxStderrBytes is the maximum number of bytes retained from a worker's stderr.
const maxStderrBytes = 1 << 20 // 1 MiB

// limitedBuffer collects up to maxStderrBytes, silently discarding the rest.
// Using a slice instead of a fixed array avoids allocating 1 MiB upfront for
// every spawned worker process.
type limitedBuffer struct {
	buf []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if remaining := maxStderrBytes - len(b.buf); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.buf = append(b.buf, p...)
	}
	return len(p), nil // always report full write so exec doesn't error
}

func (b *limitedBuffer) String() string { return string(b.buf) }

// filteredEnv returns the current environment with only the variables needed
// by worker processes, avoiding leaking the full parent environment.
func filteredEnv() []string {
	allow := []string{
		"PATH", "HOME", "USER", "SHELL", "TEMP", "TMP", "TMPDIR",
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY",
		"AEGIS_", // prefix match
		"LANG", "LC_ALL", "TERM",
		"SYSTEMROOT", "COMSPEC", "APPDATA", "LOCALAPPDATA", "USERPROFILE", // Windows
	}
	var out []string
	for _, e := range os.Environ() {
		k, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		for _, a := range allow {
			if strings.HasSuffix(a, "_") {
				if strings.HasPrefix(k, a) {
					out = append(out, e)
					break
				}
			} else if k == a {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
