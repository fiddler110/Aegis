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
}

// SubprocessBackend runs each teammate as a separate headless process (the
// harness binary invoked with WorkerArgs), giving real OS-level isolation.
// Results are read back from the teammate's mailbox after the process exits.
type SubprocessBackend struct {
	exePath     string
	workerArgs  []string
	registry    *Registry
	mailboxRoot string
	onStop      func(Identity, Result)
	wg          sync.WaitGroup
}

// OnStop registers a teammate-completion listener (SUBAGENT_STOP).
func (b *SubprocessBackend) OnStop(fn func(Identity, Result)) { b.onStop = fn }

// NewSubprocessBackend builds a subprocess backend. exePath is the harness
// executable; workerCmd is the hidden worker subcommand (e.g. "__worker").
func NewSubprocessBackend(exePath, workerCmd string, registry *Registry, mailboxRoot string) *SubprocessBackend {
	return &SubprocessBackend{
		exePath:     exePath,
		workerArgs:  []string{workerCmd},
		registry:    registry,
		mailboxRoot: mailboxRoot,
	}
}

// Spawn launches a worker process for the teammate and returns a handle.
func (b *SubprocessBackend) Spawn(ctx context.Context, cfg SpawnConfig) (*Handle, error) {
	id := NewIdentity(cfg.Name, cfg.Team, cfg.ParentSessionID)
	b.registry.Add(id)

	specPath, err := writeSpec(WorkerSpec{Identity: id, Config: cfg, MailboxRoot: b.mailboxRoot})
	if err != nil {
		b.registry.Update(id.AgentID, StatusFailed, "spec write failed")
		return nil, err
	}

	h := &Handle{Identity: id, done: make(chan Result, 1)}
	b.wg.Go(func() {
		defer os.Remove(specPath)
		res := b.runWorker(ctx, id, specPath)
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
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()

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
	f, err := os.CreateTemp("", "harness-worker-*.json")
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

// limitedBuffer collects up to 1 MiB, silently discarding the rest.
type limitedBuffer struct {
	buf [1 << 20]byte
	n   int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := copy(b.buf[b.n:], p)
	b.n += n
	return len(p), nil // always report full write so exec doesn't error
}

func (b *limitedBuffer) String() string { return string(b.buf[:b.n]) }

// filteredEnv returns the current environment with only the variables needed
// by worker processes, avoiding leaking the full parent environment.
func filteredEnv() []string {
	allow := []string{
		"PATH", "HOME", "USER", "SHELL", "TEMP", "TMP", "TMPDIR",
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY",
		"AGENTHARNESS_", // prefix match
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
