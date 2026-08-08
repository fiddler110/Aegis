//go:build live_workflow

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/server"
)

// bigRepoMapCapBytes mirrors localRepoMapMaxBytes (internal/server/helpers.go:35)
// — kept as its own constant here rather than importing the unexported
// value, since that cap is an implementation detail of internal/server, not
// a public API this package should depend on. writeBigRepoMapFixture
// self-checks its generated fixture against this value so a future change
// to the repo-map render format (or to the cap itself) fails loudly here
// instead of silently reintroducing the P28.6 false-signal bug.
const bigRepoMapCapBytes = 4000

// TestLiveWorkflow (P25.7) promotes research/eval-harness-drive.py into a Go
// test: it drives a real daemon over the exact HTTP API + SSE seam the TUI
// and web UI use — not the scripted-adapter engine.Run seam every other
// Scenario in this package exercises — against a real local model, and
// asserts workflow-shape invariants (task completed, file actually fixed,
// tool-call count/detour-freedom, non-zero token usage, no guard meta-text
// leakage, no unrequested files) rather than golden text.
//
// This is the gap the package doc calls out: TestLiveModelQuality
// (live_test.go) judges prompt/persona *quality* against a bare engine, but
// never touches the daemon/HTTP/sandbox/guard integration where the P25.1–
// P25.6 regressions actually lived — a live model driving `find /` and a
// six-approval detour because the daemon's workspace root was wrong, not
// because the model itself was slow (see the comparison table in
// research/roadmap.md's P25 section).
//
// Gated behind the live_workflow build tag (never part of `go test ./...`;
// no scheduled CI job runs it, by decision — see research/roadmap.md) since
// it needs a reachable Ollama server and a `python`/`python3` on PATH. Run it
// locally:
//
//	ollama pull qwen3.6:35b-a3b-deep   # or any tool-calling-capable local model
//	go test -tags live_workflow ./internal/eval/... -run TestLiveWorkflow -v
//
// AEGIS_EVAL_BASE_URL/AEGIS_EVAL_MODEL override the target server/model (same
// convention as live_test.go).
func TestLiveWorkflow(t *testing.T) {
	pythonExe := findPython(t)

	baseURL := os.Getenv("AEGIS_EVAL_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("AEGIS_EVAL_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	// The task itself — fixture, prompt and outcome check — lives in
	// workflowtask.go, harness-independent (P60.4), so the cross-harness
	// control group in live_workflow_baseline_test.go measures *this* task
	// rather than a second copy of it that could drift. What stays here is the
	// half that is only meaningful for Aegis: the assertions on our own SSE
	// stream.
	task := SeededBugTask()
	fixtureDir := writeSeededBugFixture(t, task)

	// Root the daemon's own default workspace at the fixture project, mirroring
	// the harness recipe's `cd <target-project> && aegis serve` — the exact
	// "daemon cwd != target project" failure mode P25.1 fixed. Sequential (no
	// t.Parallel anywhere in this file), so a single process-wide chdir for the
	// whole test is safe.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixtureDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	t.Run("FixSeededBug", func(t *testing.T) {
		cl := newLiveWorkflowDaemon(t, baseURL, model, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		guardOff := false
		events, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{
			Text:         task.Prompt(pythonExe),
			GuardEnabled: &guardOff,
		})
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}

		start := time.Now()
		summary := drainWorkflowEvents(t, events)
		elapsed := time.Since(start)
		t.Logf("run took %s, %d tool call(s): %v", elapsed, len(summary.toolCalls), summary.toolCalls)

		// Task completed: no fatal engine error.
		if summary.errText != "" {
			t.Errorf("engine reported an error: %s", summary.errText)
		}

		// File actually fixed on disk: the task's own portable outcome check
		// re-runs the script rather than trusting the model's claim of success,
		// and is the same check the baseline harness is scored by.
		if outcome := task.Outcome(fixtureDir, pythonExe); !outcome.Passed {
			for _, f := range outcome.Failures {
				t.Errorf("outcome: %s", f)
			}
		}

		// Re-run tool call observed: the task needs at least "run it", "fix
		// it", "run it again" — under-counting here means the model likely
		// never verified its own fix.
		shellCalls := 0
		for _, c := range summary.toolCalls {
			if c == "shell" {
				shellCalls++
			}
		}
		if shellCalls < 2 {
			t.Errorf("shell tool calls = %d, want >= 2 (initial run + verification re-run)", shellCalls)
		}

		// No web_search/find-style detours (the "web-search detour, `find /`
		// scan" symptom from the roadmap's live-eval writeup).
		for _, c := range summary.toolCalls {
			if c == "web_search" || c == "web_fetch" {
				t.Errorf("unexpected network tool call %q for a file-scoped fix task", c)
			}
		}
		for _, cmd := range summary.shellCommands {
			if strings.Contains(cmd, "find /") {
				t.Errorf("shell command looks like an unscoped filesystem scan: %q", cmd)
			}
		}

		// Tool-call count budget: generous enough to tolerate a slower/less
		// capable model's extra diagnostic step, tight enough to catch a
		// runaway loop.
		const maxToolCalls = 20
		if len(summary.toolCalls) > maxToolCalls {
			t.Errorf("tool calls = %d, want <= %d; calls: %v", len(summary.toolCalls), maxToolCalls, summary.toolCalls)
		}

		// No unrequested files/remember calls (P25.6): every write/edit must
		// target temps.py, the only file the task named.
		if summary.rememberCalls > 0 {
			t.Errorf("unprompted remember call(s): %d", summary.rememberCalls)
		}
		for _, p := range summary.writtenPaths {
			if p != "temps.py" && p != "" {
				t.Errorf("unrequested file touched: %q (task only asked about temps.py)", p)
			}
		}

		// Non-zero token usage on done (P25.5).
		if summary.inputTokens+summary.outputTokens == 0 {
			t.Error("done event reported zero input+output tokens")
		}

		// <= 2 approvals in build mode (P25.4): with auto_approve_exec set, the
		// daemon should never even emit an approval_request in the first place.
		if summary.approvals > 2 {
			t.Errorf("approval requests = %d, want <= 2 under auto-approve", summary.approvals)
		}
	})

	t.Run("GuardNoMetaLeak", func(t *testing.T) {
		cl := newLiveWorkflowDaemon(t, baseURL, model, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		guardOn := true
		events, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{
			Text:         task.Prompt(pythonExe),
			GuardEnabled: &guardOn,
		})
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
		summary := drainWorkflowEvents(t, events)
		if summary.errText != "" {
			t.Errorf("engine reported an error with the guard on: %s", summary.errText)
		}
		// P25.3: a guard verdict retry must never leak its own PASS/FAIL
		// meta-language into the answer the user actually sees.
		for _, bad := range []string{"PASS.", "PASS\n", "FAIL:", "VERDICT:"} {
			if strings.Contains(summary.finalText, bad) {
				t.Errorf("final answer leaks guard meta-text %q:\n%s", bad, summary.finalText)
			}
		}
	})

	t.Run("LocalPromptProfileReducesFirstTurnTokens", func(t *testing.T) {
		// A trivial, tool-free prompt isolates the system-prompt/tool-schema
		// overhead the P25.6 profile trims (deferred web_fetch/web_search/
		// security_scan/git_pr schemas, capped repo map) from anything
		// task-specific, so the two runs are an apples-to-apples comparison of
		// prompt shape alone.
		const trivialPrompt = "Reply with only the single word OK."

		// P28.6: the shared fixtureDir this whole test chdir'd into (temps.py
		// + temps.csv, a couple hundred bytes) never comes close to tripping
		// the local profile's repo-map cap (internal/server/helpers.go:35),
		// so both profiles produced byte-identical system prompts here and
		// the token comparison below degenerated into pure live-model
		// token-accounting noise (observed: passed for gpt-oss:20b, failed
		// for deepseek-r1:8b on the same code). Swap in a dedicated, larger
		// workspace that actually crosses the cap so "local" and "default"
		// produce genuinely different prompts — local omits the oversized
		// repo map entirely, default injects it in full — and the token
		// comparison is a real signal again. Scoped to this subtest only
		// (chdir restored on cleanup); FixSeededBug/GuardNoMetaLeak above
		// already ran against the original small fixture.
		bigDir := writeBigRepoMapFixture(t)
		origWD, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(bigDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWD) })

		// P63.11: this subtest measures prompt size through the model's reported
		// prompt_eval_count, and that number is clamped at the served num_ctx. On
		// qwen3:14b at Ollama's default 4096 window both profiles reported exactly
		// 4095 and the comparison below failed while naming P25.6 as the cause —
		// but P25.6 was working; the *instrument* had saturated, because both
		// prompts exceeded the window. Pin a window large enough that neither
		// prompt reaches it, and verify below that the pin actually took.
		//
		// The byte-level property (local omits an oversized repo map and assembles
		// a shorter prompt) is already asserted without a live model in
		// TestEffectiveSystem_localProfileTrimsPrompt; what only this subtest can
		// show is that the smaller prompt reaches the provider, which is why the
		// measurement stays end-to-end rather than moving to a byte comparison.
		//
		// Unloading first is what makes the pin take. The daemon deliberately
		// refuses to promise more window than Ollama is *currently serving*
		// (applyDetectedWindowFor: a loaded-model reading is authoritative and
		// wins over config, since trusting config there is the silent-truncation
		// failure), so a resident 4096 instance defeats the pin — measured, that
		// is exactly what happened: "configured context_window exceeds what
		// Ollama is serving; using the served value ... configured=16384
		// served=4096". With no instance loaded, detection is non-authoritative,
		// config wins, and the model reloads at the requested window on the first
		// request. The cost is evicting whatever the developer had resident.
		unloadOllamaModel(t, baseURL, model)

		localCl := newLiveWorkflowDaemonWithWindow(t, baseURL, model, "local", promptProfileNumCtx)
		defaultCl := newLiveWorkflowDaemonWithWindow(t, baseURL, model, "default", promptProfileNumCtx)

		firstTurnTokens := func(cl *client.Client) int {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			events, err := cl.PostMessage(ctx, meta.ID, trivialPrompt)
			if err != nil {
				t.Fatalf("PostMessage: %v", err)
			}
			summary := drainWorkflowEvents(t, events)
			return summary.inputTokens
		}

		localTokens := firstTurnTokens(localCl)
		defaultTokens := firstTurnTokens(defaultCl)
		localWin, localWinSrc := servedContextWindow(t, localCl)
		defaultWin, defaultWinSrc := servedContextWindow(t, defaultCl)
		t.Logf("first-turn input tokens: local=%d (window %d from %s) default=%d (window %d from %s)",
			localTokens, localWin, localWinSrc, defaultTokens, defaultWin, defaultWinSrc)
		if localTokens == 0 || defaultTokens == 0 {
			t.Fatal("expected non-zero input token counts from both profiles")
		}

		// Saturation must be reported as saturation. A count pinned at the served
		// window says the server truncated the prompt and reported the clamp, so
		// the two profiles are indistinguishable no matter how differently sized
		// they really are — a fact about the instrument, not about P25.6.
		//
		// Skipped rather than failed, matching how findPython treats a missing
		// interpreter in this same on-demand suite: everything this test can do to
		// get a measurable window (pin it, unload the blocking instance) has
		// already been done, so what is left is a server that will not serve the
		// window, which is an environment gap. A SKIP still reads as "asserted
		// nothing" in the output, which is the property that matters — the failure
		// mode this replaces was a red naming P25.6 for a saturated instrument.
		if why := saturationReason(localTokens, localWin, defaultTokens, defaultWin); why != "" {
			t.Skipf("measurement saturated, so this subtest cannot say anything about the local prompt profile: %s\n"+
				"local=%d (window %d from %s), default=%d (window %d from %s)\n"+
				"the daemon asked for num_ctx=%d and unloaded the model first; a smaller served window means the server caps it — "+
				"raise OLLAMA_CONTEXT_LENGTH on the Ollama server or pin num_ctx in a modelfile",
				why, localTokens, localWin, localWinSrc, defaultTokens, defaultWin, defaultWinSrc, promptProfileNumCtx)
		}

		if localTokens >= defaultTokens {
			t.Errorf("local prompt profile did not reduce first-turn input tokens: local=%d, default=%d (neither count is clamped, so this is a real difference in prompt size)", localTokens, defaultTokens)
		}
	})
}

// findPython locates a usable Python interpreter, skipping the test (rather
// than failing it) when none is found — this suite is on-demand only, and a
// missing interpreter is an environment gap, not a regression. Every
// candidate is actually invoked (`--version`), not just resolved via PATH:
// on Windows, `python3`/`python` on PATH commonly resolve to the App
// Execution Alias stub (WindowsApps\python3.exe), which exec.LookPath finds
// happily but which errors out ("Python was not found; run without
// arguments to install from the Microsoft Store...") the moment it's
// actually run without a real interpreter behind it.
func findPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--version").Run(); err == nil {
			return path
		}
	}
	t.Skip("no working python3/python on PATH; skipping live workflow test")
	return ""
}

// writeSeededBugFixture materializes task into a fresh directory. The content
// itself is SeededBugTask's (workflowtask.go) — the two-file temps.py/temps.csv
// project from the P25 live-eval writeup, where the "temp" column comes back as
// a string and is added to an int accumulator without converting first.
//
// Deliberately not t.TempDir(): this directory becomes the daemon's own
// default workspace (the test chdir's into it), and server.New opens a
// knowledge.db/longmem.db under it that nothing in this test closes —
// t.TempDir()'s automatic cleanup treats a locked file as a hard test
// failure (fails on Windows, where an open handle blocks deletion outright),
// not a log line. Match dataDir's self-managed, best-effort-cleanup pattern
// in newLiveWorkflowDaemon instead.
func writeSeededBugFixture(t *testing.T, task WorkflowTask) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aegis-live-workflow-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("cleanup: could not remove fixture dir %s: %v", dir, rmErr)
		}
	})
	if err := task.Materialize(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeBigRepoMapFixture (P28.6) builds a workspace whose indexed repo map
// comfortably exceeds bigRepoMapCapBytes, so the "local" and "default"
// prompt profiles actually diverge: local (internal/server/helpers.go:62)
// omits an oversized repo map entirely, default always injects it in full.
// It writes enough filler Python files/functions for that, then pre-builds
// and saves the repomap.json cache directly — what `aegis index` (or the
// daemon's own startup loadRepoMap) would produce — so the daemon picks up
// the map on process start without an extra round trip through the index
// endpoint.
//
// Deliberately not t.TempDir(), matching writeSeededBugFixture: this
// directory becomes the daemon's own workspace and gets a
// `.aegis/knowledge.db` opened under it that nothing in this test closes.
func writeBigRepoMapFixture(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aegis-live-workflow-bigrepo-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("cleanup: could not remove big-repo-map fixture dir %s: %v", dir, rmErr)
		}
	})

	// The fixture must clear bigRepoMapCapBytes (4000) while staying under
	// repomap's own render budget (repomap.DefaultMaxBytes, 8000), so the two
	// profiles end up genuinely "full map" vs. "no map" rather than "full map"
	// vs. "truncated map".
	//
	// It used to be a fixed 15 files x 10 functions, and P62.1 silently
	// invalidated that: each file now contributes at most
	// repomap.DefaultMaxSymbolsPerFile symbols, so the same fixture rendered to
	// 2154 bytes — under the cap, with the subtest asserting nothing. Size in
	// *files* rather than in functions-per-file, give each file exactly the
	// per-file cap so no symbol list is cut (a "+N more" marker would make this
	// the truncated map the paragraph above rules out), and grow until the
	// rendered block actually clears the threshold rather than trusting a
	// hand-computed count to survive the next render change.
	perFile := repomap.DefaultMaxSymbolsPerFile
	var m *repomap.Map
	for files := 40; ; files += 10 {
		if files > 200 {
			t.Fatalf("could not size a fixture above %d bytes without exceeding repomap's own %d-byte budget", bigRepoMapCapBytes, repomap.DefaultMaxBytes)
		}
		for i := range files {
			var b strings.Builder
			for j := range perFile {
				fmt.Fprintf(&b, "def handler_%02d_%02d(request, context):\n    pass\n\n", i, j)
			}
			path := filepath.Join(dir, fmt.Sprintf("module_%03d.py", i))
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		built, err := repomap.Build(dir, repomap.Options{})
		if err != nil {
			t.Fatalf("repomap.Build: %v", err)
		}
		m = built
		if len(repomap.Block(m.Render())) > bigRepoMapCapBytes {
			break
		}
	}

	cache := filepath.Join(dir, ".aegis", "repomap.json")
	if err := m.Save(cache); err != nil {
		t.Fatalf("repomap.Save: %v", err)
	}
	rendered := m.Render()
	if got := len(repomap.Block(rendered)); got <= bigRepoMapCapBytes {
		t.Fatalf("fixture repo map too small to trigger the local-profile cap: rendered block is %d bytes, want > %d", got, bigRepoMapCapBytes)
	}
	// The "not truncated" half of the requirement, which the byte assertion
	// above cannot see: a map that hit either truncation is a different
	// comparison from the one this subtest claims to make.
	if strings.Contains(rendered, "truncated") || strings.Contains(rendered, "more") {
		t.Fatalf("fixture repo map is truncated, so the profiles differ by more than presence/absence:\n%s", rendered)
	}
	return dir
}

// newLiveWorkflowDaemon builds a real daemon (server.New — full production
// wiring, not the synthetic newWithDeps internal/server tests use) rooted at
// the process's current working directory, serving over an in-process
// httptest.Server the same way the TUI/web UI reach a real daemon: HTTP +
// SSE, bearer-token authenticated. promptProfile forces provider.prompt_profile
// ("local"/"default"); "" leaves it on auto-detect (loopback baseURL implies
// "local", matching a real Ollama setup with no explicit override).
//
// The context window is left unpinned (auto-detected from the server), which is
// what the workflow subtests want: they measure model behavior, and a pinned
// window would only cost VRAM on the box running them. Only the token-accounting
// subtest needs a specific window — see newLiveWorkflowDaemonWithWindow.
func newLiveWorkflowDaemon(t *testing.T, baseURL, model, promptProfile string) *client.Client {
	t.Helper()
	return newLiveWorkflowDaemonWithWindow(t, baseURL, model, promptProfile, 0)
}

// newLiveWorkflowDaemonWithWindow is newLiveWorkflowDaemon with an explicit
// provider.context_window (0 = auto-detect). The daemon sends it to Ollama as
// num_ctx, so it decides how much prompt the server will actually accept — and
// therefore whether a prompt-size measurement taken through the model's own
// prompt_eval_count is meaningful at all (P63.11).
func newLiveWorkflowDaemonWithWindow(t *testing.T, baseURL, model, promptProfile string, contextWindow int) *client.Client {
	t.Helper()

	// A self-managed temp dir with best-effort (non-fatal) cleanup: server.New
	// never gets a chance to close its own sqlite handle here (there's no
	// exported Close/Shutdown short of running the full ListenAndServe
	// lifecycle this test deliberately bypasses), and t.TempDir()'s cleanup
	// fails the test outright if RemoveAll can't delete a still-open file —
	// a real risk on Windows, where an open handle blocks deletion outright.
	dataDir, err := os.MkdirTemp("", "aegis-live-workflow-data-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dataDir); rmErr != nil {
			t.Logf("cleanup: could not remove data dir %s: %v", dataDir, rmErr)
		}
	})

	cfg := &config.Config{
		DataDir: dataDir,
		Provider: config.ProviderConfig{
			Default:       "ollama",
			BaseURL:       baseURL,
			Model:         model,
			MaxTokens:     4096,
			ContextWindow: contextWindow,
			PromptProfile: promptProfile,
		},
		Permission: config.PermissionConfig{
			Mode:            "build",
			AutoApproveExec: true,
			// This daemon's sandbox is unconfigured (unsandboxed local) and its
			// workspace is a throwaway fixture directory — an intentional,
			// understood choice for this on-demand test, not production use
			// (see the P25.2 startup refusal this opts out of).
			AllowUnsandboxedAutoExec: true,
		},
		OutputGuard: config.OutputGuardConfig{
			Mode:   "llm",
			Rubric: config.DefaultGuardRubric,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv, err := server.New(cfg, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return client.New(ts.URL).WithTokenFile(cfg.AuthTokenPath())
}

// unloadOllamaModel asks Ollama to evict model from memory (`keep_alive: 0` on
// an empty generate request), so the next request reloads it at whatever num_ctx
// that request carries. It exists for P63.11: a resident instance's window is
// authoritative and overrides the daemon's configured one, so a model left
// loaded at 4096 by an earlier run makes a prompt-size measurement impossible.
//
// Best-effort by design — a non-Ollama backend has no such endpoint and a
// failure here only means the pin may not take, which the saturation check
// downstream reports far more precisely than a guess at this layer could.
func unloadOllamaModel(t *testing.T, baseURL, model string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	body := fmt.Sprintf(`{"model":%q,"keep_alive":0}`, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(baseURL, "/")+"/api/generate", strings.NewReader(body))
	if err != nil {
		t.Logf("could not build unload request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("could not unload %s (the configured context window may not take): %v", model, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Logf("unload of %s returned %s (the configured context window may not take)", model, resp.Status)
		return
	}

	// Eviction is not complete when the response returns, and the gap is large
	// enough to matter: measured, a daemon built ~100ms later still detected the
	// old instance ("configured=16384 served=4096" from source ollama:loaded)
	// and pinned itself to the window that was supposed to have gone away. Wait
	// for /api/ps to actually stop listing it rather than sleeping a guessed
	// interval.
	deadline := time.Now().Add(30 * time.Second)
	for {
		loaded, ok := ollamaModelLoaded(baseURL, model)
		if ok && !loaded {
			t.Logf("unloaded %s so it reloads at the requested num_ctx", model)
			return
		}
		if !ok || time.Now().After(deadline) {
			t.Logf("could not confirm %s was unloaded (the configured context window may not take)", model)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ollamaModelLoaded reports whether /api/ps currently lists model — the same
// signal internal/ollamainfo treats as the authoritative context window, which
// is what makes it the right thing to wait on. ok is false when /api/ps could
// not be read at all, so a caller can tell "not loaded" from "don't know".
func ollamaModelLoaded(baseURL, model string) (loaded, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/api/ps", nil)
	if err != nil {
		return false, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, false
	}
	var out struct {
		Models []struct {
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, false
	}
	for _, m := range out.Models {
		if m.Model == model || m.Name == model {
			return true, true
		}
	}
	return false, true
}

// servedContextWindow reports the context window the daemon actually resolved
// for its model, and where that value came from ("config" when the pin took,
// "ollama:loaded" and friends when the server's own reading won). It is what
// tells a saturated prompt-size measurement apart from a real one (P63.11); an
// unreachable or silent /status is not worth failing the test over, so it
// degrades to an unknown window (0) and the caller's weaker heuristic.
func servedContextWindow(t *testing.T, cl *client.Client) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := cl.StatusInfo(ctx)
	if err != nil {
		t.Logf("could not read served context window: %v", err)
		return 0, "unknown"
	}
	src := st.ContextWindowSource
	if src == "" {
		src = "unknown"
	}
	return st.ContextWindow, src
}

// workflowSummary is the reduction of one message run's SSE events into the
// fields TestLiveWorkflow's invariants check.
type workflowSummary struct {
	finalText     string
	errText       string
	toolCalls     []string
	shellCommands []string
	writtenPaths  []string // path argument of every write_file/edit_file call
	rememberCalls int
	approvals     int
	inputTokens   int
	outputTokens  int
}

// drainWorkflowEvents consumes a message run's event channel to completion
// and reduces it to a workflowSummary, logging a compact timeline as it goes
// (visible under `go test -v`) the same way the Python harness printed one.
func drainWorkflowEvents(t *testing.T, events <-chan api.Event) workflowSummary {
	t.Helper()
	var s workflowSummary
	start := time.Now()
	for ev := range events {
		elapsed := time.Since(start)
		switch ev.Kind {
		case api.KindText:
			s.finalText += ev.Text
		case api.KindToolCall:
			s.toolCalls = append(s.toolCalls, ev.Tool)
			t.Logf("[%7.1fs] tool_call %s %s", elapsed.Seconds(), ev.Tool, truncateForLog(string(ev.ToolInput), 160))
			switch ev.Tool {
			case "shell":
				var args struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(ev.ToolInput, &args) == nil {
					s.shellCommands = append(s.shellCommands, args.Command)
				}
			case "write_file", "edit_file":
				var args struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(ev.ToolInput, &args) == nil {
					s.writtenPaths = append(s.writtenPaths, args.Path)
				}
			case "remember":
				s.rememberCalls++
			}
		case api.KindToolResult:
			status := "ok"
			if ev.ToolIsError {
				status = "ERR"
			}
			t.Logf("[%7.1fs] tool_result %s (%d chars)", elapsed.Seconds(), status, len(ev.ToolResult))
		case api.KindApprovalRequest:
			s.approvals++
			t.Logf("[%7.1fs] APPROVAL_REQUEST %s — auto-approve should have handled this", elapsed.Seconds(), ev.Tool)
		case api.KindError:
			s.errText = ev.Error
			t.Logf("[%7.1fs] ERROR %s", elapsed.Seconds(), ev.Error)
		case api.KindDone:
			s.inputTokens = ev.InputTokens
			s.outputTokens = ev.OutputTokens
			t.Logf("[%7.1fs] done in=%d out=%d estimated=%v", elapsed.Seconds(), ev.InputTokens, ev.OutputTokens, ev.TokensEstimated)
		}
	}
	return s
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
