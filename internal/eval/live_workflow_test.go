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

		// Refuse to measure through a window too small to hold the task, the
		// same way the profile subtest refuses to measure a saturated prompt.
		// This daemon leaves provider.context_window unset on purpose (it is
		// testing the default path), so it plans against whatever detection
		// found — 4,096 when the model is not loaded and pins no num_ctx, which
		// on 2026-08-14 produced a red that named the model and the harness for
		// what was an environment gap.
		if win, src := servedContextWindow(t, cl); insufficientWindowReason(win, src) != "" {
			t.Skip(insufficientWindowReason(win, src))
		}

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
	return newLiveWorkflowDaemonTweaked(t, baseURL, model, promptProfile, contextWindow, nil)
}

// newLiveWorkflowDaemonTweaked is newLiveWorkflowDaemonWithWindow with a hook
// to adjust the assembled config before server.New sees it, for the one
// subtest that needs two daemons differing in a single setting (P62.2).
func newLiveWorkflowDaemonTweaked(t *testing.T, baseURL, model, promptProfile string, contextWindow int, tweak func(*config.Config)) *client.Client {
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

	if tweak != nil {
		tweak(cfg)
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
	// notices is every engine advisory the run emitted, in order — the record
	// that says whether compaction, the loop detector or a nudge actually fired,
	// without which a failed subtest cannot be attributed.
	notices []string
	// turns is one entry per model turn, carrying the prompt size the provider
	// actually saw and how long it spent prefilling it (P62.2). The pair is the
	// whole measurement: a prefix-cache hit is a large prompt with a tiny
	// prefill; a mid-history rewrite is a similar prompt an order of magnitude
	// slower.
	turns        []turnMetric
	inputTokens  int
	outputTokens int
}

// turnMetric is one model turn's prompt size and prefill cost.
type turnMetric struct {
	inputTokens  int
	promptEvalMS int64
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
		case api.KindNotice:
			// Notices were dropped on the floor until 2026-08-08, which made the
			// tier structurally blind to the engine's own advisories: compaction,
			// the context-full warning, loop-detector nudges, tool-failure
			// correctives, shim warnings. That is exactly the evidence needed to
			// attribute a subtest failure — a P63.9 pass over compaction could
			// not be told apart from a model failure, because the log could not
			// say whether compaction ran at all. The daemon logger here is at
			// WARN, so this is the only place they surface.
			s.notices = append(s.notices, ev.Text)
			t.Logf("[%7.1fs] notice %s", elapsed.Seconds(), truncateForLog(ev.Text, 200))
		case api.KindApprovalRequest:
			s.approvals++
			t.Logf("[%7.1fs] APPROVAL_REQUEST %s — auto-approve should have handled this", elapsed.Seconds(), ev.Tool)
		case api.KindError:
			s.errText = ev.Error
			t.Logf("[%7.1fs] ERROR %s", elapsed.Seconds(), ev.Error)
		case api.KindTurnDone:
			s.turns = append(s.turns, turnMetric{inputTokens: ev.InputTokens, promptEvalMS: ev.PromptEvalDurationMS})
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

// The P62.2 measurement fixture. Three numbers, sized together so that
// compaction fires with real history behind it rather than at a conversation
// too short to have anything to summarize.
const (
	// compactionNumCtx has to leave room for the conversation to *grow*, which
	// is a stronger constraint than "small". Measured on qwen3:14b, the base
	// prompt — system prompt, tool schemas, repo map — is **7,119 tokens** before
	// a single file is read. At the 8192 this fixture first used, that is 87% of
	// the window gone at turn zero: the chain got four reads in before the model
	// was squeezed out of room and abandoned it, and compaction never fired
	// because nothing ever accumulated ahead of the keepRecent tail.
	//
	// So the window is sized as base + enough growth to cross the trigger with
	// history to spare, not as small as possible.
	compactionNumCtx = 24576
	// compactionMaxTokens bounds the completion reserve. compactionTrigger is
	// min(85% of window, window - maxTokens - margin), so a large reserve against
	// a small window drags the trigger below the base prompt and makes the engine
	// ask for compaction on turn one, before any history exists — one of the two
	// defects that made this fixture's first version measure nothing. At this
	// window the 85% cap binds instead, putting the trigger at ~20.9k.
	// It must also stay **above 20% of the window**, which is the non-obvious
	// half. The prune gate this test exists to measure fires at est > 75% of the
	// window, while the engine only *calls* Compact() at
	// min(85%, window - maxTokens - 5%). If maxTokens is small, that min resolves
	// to 85% — above the gate — so by the time compaction is invoked the gate is
	// always already satisfied and both arms prune identically. Measured: at
	// maxTokens 2048 against this window (8%), the two arms came out at 141,611ms
	// vs 142,156ms of prefill, i.e. the A/B was inoperative by construction.
	// Above 20% the trigger drops below the gate and there is a real span where
	// they differ — which is the shape of the default config (32,768 against a
	// ~41k local window, 80%) that P62.2's original drive measured.
	compactionMaxTokens = 8192
	// compactionChainLen is how many files the chain walks. Each read is its own
	// turn (see writeCompactionFixture), so this also bounds the message count
	// the summarizer gets to work with — it must comfortably exceed compaction's
	// keepRecent tail of 8, or there is nothing ahead of the tail to summarize.
	compactionChainLen = 14
)

// writeCompactionFixture builds the adversarial fixture P62.2 has been owed
// since it shipped: a workspace whose tool output reliably grows the
// conversation past the compaction trigger, across enough *turns* that there is
// history to compact when it does.
//
// The chain is the point. The obvious fixture — N files and "read them one at a
// time" — does not work, and measuring is how that surfaced: read_file declares
// CapRead, so engine.runTools dispatches the whole round concurrently, and
// qwen3:14b duly emitted all 8 reads in a single turn. The result was a 2-turn
// run at 129% of the window reporting "nothing left to compact", because
// everything there was still inside the keepRecent tail. Sequencing is not
// something a prompt can ask for here; it has to be impossible to batch.
//
// So each file's last line names the next one, and the order is not guessable
// from the current filename. The model cannot read file N+1 until file N comes
// back, which makes one read per turn a property of the fixture rather than a
// request.
//
// P62.2's write-up rules out the alternatives: waiting for the condition on a
// real drive (three drives produced three different workloads) and restoring
// on-disk state that once triggered it (a fresh process ran the same state to a
// completely different, working strategy). Forcing it structurally is what is
// left.
func writeCompactionFixture(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aegis-live-workflow-compaction-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("cleanup: could not remove compaction fixture dir %s: %v", dir, err)
		}
	})

	// A fixed, non-sequential visiting order so the next filename cannot be
	// guessed from the current one. Deterministic (no rand) so both arms of the
	// A/B walk an identical chain.
	order := []int{1, 9, 4, 12, 7, 2, 14, 5, 11, 3, 13, 6, 10, 8}
	if len(order) != compactionChainLen {
		t.Fatalf("chain order has %d entries, want compactionChainLen (%d)", len(order), compactionChainLen)
	}
	for pos, n := range order {
		var b strings.Builder
		fmt.Fprintf(&b, "# Record %02d (step %d of %d)\n\n", n, pos+1, len(order))
		// ~60 lines of distinct prose, about 1,950 tokens per file. Sized from the
		// measured base (7,119) and trigger (~20,889): the chain crosses the
		// trigger around its eighth read, leaving six more reads to happen *after*
		// compaction has run, and well over keepRecent's 8 messages of history
		// ahead of the tail by the time it does.
		for line := 1; line <= 60; line++ {
			fmt.Fprintf(&b, "record %02d line %02d: measurement %d at station %d, tolerance %d.%d\n",
				n, line, n*1000+line, line*7%13, n, line)
		}
		if pos+1 < len(order) {
			fmt.Fprintf(&b, "\nnext: data_%02d.txt\n", order[pos+1])
		} else {
			b.WriteString("\nnext: END\n")
		}
		name := filepath.Join(dir, fmt.Sprintf("data_%02d.txt", n))
		if err := os.WriteFile(name, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func compactionPrompt() string {
	return "Read the file data_01.txt with the read_file tool. The last line of every file names the " +
		"next file to read. Follow that chain one file at a time, reading each file as you reach it, " +
		"until you reach a file whose last line is 'next: END'. Do not guess filenames and do not read " +
		"a file before the chain leads you to it. When the chain ends, reply with the single word DONE."
}

// TestLiveWorkflowCompactionPrefixCacheGate is the P62.2 measurement: run the
// same forced-compaction workload twice, changing only
// compaction.preserve_prefix_cache, and report what the gate costs and buys.
//
// **It asserts mechanism and reports measurement.** The assertions are that
// both arms completed and that compaction *actually ran*, because a run where
// it never ran measures nothing — which is how P63.9's compaction pass came to
// be "validated" by a tier that never exercised compaction, and how this
// fixture's own first version came back green and empty. The wall-clock and
// prefill comparison is logged, not asserted: one A/B against a local model is
// a sample of one, and a test that failed on a timing delta would be flaky in
// both directions. The decision P62.2 asks for is a judgement made from this
// output, not something a green tick can stand in for.
func TestLiveWorkflowCompactionPrefixCacheGate(t *testing.T) {
	baseURL := os.Getenv("AEGIS_EVAL_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("AEGIS_EVAL_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	fixtureDir := writeCompactionFixture(t)
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixtureDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	type arm struct {
		name     string
		gate     bool
		wall     time.Duration
		summary  workflowSummary
		shrank   int
		prefillM int64
	}
	run := func(name string, gate bool) arm {
		// Same reason as the prompt-profile subtest: a resident instance's
		// window wins over config, so the pin only takes with nothing loaded
		// (P63.11). Per arm, not once — the first arm leaves the model resident
		// for the second.
		unloadOllamaModel(t, baseURL, model)
		cl := newLiveWorkflowDaemonTweaked(t, baseURL, model, "", compactionNumCtx, func(cfg *config.Config) {
			cfg.Compaction.PreservePrefixCache = &gate
			cfg.Provider.MaxTokens = compactionMaxTokens
		})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
		if err != nil {
			t.Fatalf("%s: CreateSession: %v", name, err)
		}
		guardOff := false
		events, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{
			Text:         compactionPrompt(),
			GuardEnabled: &guardOff,
		})
		if err != nil {
			t.Fatalf("%s: PostMessage: %v", name, err)
		}
		// Log the window the daemon actually resolved. Without it, a trigger that
		// never fires has two indistinguishable explanations — the estimate
		// undercounting the real prompt, or the engine measuring against a
		// different window than Ollama is serving — and run 3 could not tell them
		// apart.
		if win, src := servedContextWindow(t, cl); win != compactionNumCtx {
			t.Logf("%s: WARNING daemon resolved context window %d (from %s), not the pinned %d",
				name, win, src, compactionNumCtx)
		} else {
			t.Logf("%s: context window %d (from %s)", name, win, src)
		}
		start := time.Now()
		s := drainWorkflowEvents(t, events)
		a := arm{name: name, gate: gate, wall: time.Since(start), summary: s}
		// A turn whose prompt shrank against the previous one is a turn whose
		// history was rewritten — the event the gate exists to make rare.
		for i := 1; i < len(s.turns); i++ {
			if s.turns[i].inputTokens < s.turns[i-1].inputTokens {
				a.shrank++
			}
			a.prefillM += s.turns[i].promptEvalMS
		}
		return a
	}

	on := run("gate-on", true)
	off := run("gate-off", false)

	for _, a := range []arm{on, off} {
		t.Logf("--- %s (preserve_prefix_cache=%v): wall=%s turns=%d shrinking-turns=%d prefill-total=%dms reads=%d",
			a.name, a.gate, a.wall.Round(time.Second), len(a.summary.turns), a.shrank, a.prefillM,
			countToolCalls(a.summary.toolCalls, "read_file"))
		for i, tm := range a.summary.turns {
			t.Logf("      turn %2d: prompt=%6d prefill=%6dms", i, tm.inputTokens, tm.promptEvalMS)
		}
		for _, n := range a.summary.notices {
			t.Logf("      notice: %s", n)
		}
	}
	t.Logf("=== P62.2: wall %s (gate on) vs %s (gate off); shrinking turns %d vs %d; prefill %dms vs %dms",
		on.wall.Round(time.Second), off.wall.Round(time.Second), on.shrank, off.shrank, on.prefillM, off.prefillM)

	for _, a := range []arm{on, off} {
		if a.summary.errText != "" {
			t.Errorf("%s: engine reported an error: %s", a.name, a.summary.errText)
		}
		// The instrument check. "nothing left to compact" is the notice this
		// fixture's first version accepted as proof that compaction had run; it
		// says the opposite, and matching it explicitly stops a future edit from
		// re-earning a green that measures nothing.
		compacted, starved := false, false
		for _, n := range a.summary.notices {
			switch {
			case strings.Contains(n, "nothing left to compact"):
				starved = true
			case strings.Contains(n, "compacted"):
				compacted = true
			}
		}
		if starved {
			t.Errorf("%s: the run hit the window with nothing to summarize — the conversation crossed the "+
				"trigger before it had history past compaction's keepRecent tail; lengthen the chain or "+
				"shrink the per-file payload", a.name)
		}
		if !compacted {
			t.Errorf("%s: no compaction actually ran, so this run measures nothing about the gate "+
				"(turns=%d, reads=%d)", a.name, len(a.summary.turns),
				countToolCalls(a.summary.toolCalls, "read_file"))
		}
		// The chain is what forces one read per turn; if the model bailed early
		// there was never enough growth to matter.
		if got := countToolCalls(a.summary.toolCalls, "read_file"); got < compactionChainLen/2 {
			t.Errorf("%s: only %d read_file calls of a %d-file chain — the model abandoned the chain, so the "+
				"conversation never grew as designed", a.name, got, compactionChainLen)
		}
	}
}

func countToolCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

// forcedOverflowNumCtx is sized from the base prompt, not chosen for being
// small — the same constraint writeCompactionFixture records above, and one that
// is easy to get wrong in the opposite direction here.
//
// The condition being forced is a generation cut off *mid-tool-call* at the
// context ceiling, which is the only shape of overflow a local Ollama server
// surfaces as an error at all: an oversized *prompt* is silently
// front-truncated instead, which is P62.4's whole finding. So the window has to
// leave the prompt intact and starve only the generation.
//
// The base prompt — system prompt, tool schemas, repo map — measures 7,119
// tokens on qwen3:14b before any work happens. Anything at or below that
// truncates the prompt itself, and the model then fails for a reason that has
// nothing to do with the tool call. 8192 clears the base with ~1,000 tokens of
// generation headroom: far more than a normal reply needs, and far less than the
// write below asks for.
const forcedOverflowNumCtx = 8192

// forcedOverflowPrompt asks for a single tool call whose arguments cannot fit
// the remaining budget. It names a mechanical, unambiguous task on purpose:
// the model must not be able to satisfy it briefly, summarize it, or decide it
// is done early, or the fixture measures nothing.
func forcedOverflowPrompt() string {
	return "Use the write_file tool exactly once to create a file called numbers.txt whose content is " +
		"the numbers from 1 to 4000, one per line, written out in full with no ranges, no ellipses and " +
		"no abbreviation. Put the entire content in the single write_file call."
}

// TestLiveWorkflowForcedContextOverflow is the live half of P62.5.
//
// The ladder it exists to validate (OverflowEscalationDirective +
// maxPhaseOverflowResets, P62.3) is driven end to end by
// TestOverflowLadderClimbsThenStops in internal/drive, which asserts the
// sequence of rungs a run is retried with. That test fakes exactly one link:
// it hands the drive an error it has *declared* to be a context overflow. This
// test covers that link against a real model — a real generation, cut off at a
// real context ceiling, must come back classified as an overflow, because the
// entire recovery path is gated on that classification and nothing else.
//
// It asserts a classification, not a recovery. Whether a given local model
// truncates on a given turn is a property of that model's verbosity, so a hard
// assertion that the overflow *occurred* would be flaky in a way no amount of
// prompt tuning fixes. Instead: if the run failed, the failure must be the
// actionable truncation error rather than the opaque "invalid tool call
// arguments … unexpected end of JSON input" the server actually sends — which
// is the whole point of P35.2 and the thing that would silently rot. If the run
// did not overflow, the test says so and skips, rather than passing quietly.
//
// # Status: NOT YET OBSERVED TO FIRE — read this before trusting it
//
// Attempted 2026-08-09 against qwen3:14b at the window below and abandoned after
// ~30 minutes without reaching a verdict. The model did not answer with one
// oversized tool call. It appears to have answered in *text*, hit max_tokens,
// and taken the "continue from where you left off" path — which regrows the
// context and repeats, walking toward maxIterations instead of truncating a tool
// call. That is a real path and arguably worth its own coverage, but it is not
// the path this test exists to exercise, and it makes the test far too slow to
// be useful as written.
//
// So this fixture is **unvalidated scaffolding**, kept because the seam it aims
// at is real and the setup is most of the work, not because it has demonstrated
// anything. Before relying on it, either force a tool call structurally (a tool
// whose schema requires a large argument, rather than a prompt that asks for a
// large file) or pick a model that reliably emits one. Do not read a skip from
// this test as evidence that classification works.
//
// The mechanism P62.5 needed is covered deterministically and does not depend on
// this: internal/drive's TestOverflowLadderClimbsThenStops drives the real loop
// end to end. What stays uncovered live is only the classification link — that a
// real Ollama truncation is recognised as an overflow rather than as a malformed
// call.
func TestLiveWorkflowForcedContextOverflow(t *testing.T) {
	baseURL := os.Getenv("AEGIS_EVAL_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("AEGIS_EVAL_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	dir, err := os.MkdirTemp("", "aegis-live-workflow-overflow-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("cleanup: could not remove overflow fixture dir %s: %v", dir, err)
		}
	})
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Same reason as every other window-pinning subtest here: a resident
	// instance's window wins over config, so the pin only takes with nothing
	// loaded (P63.11).
	unloadOllamaModel(t, baseURL, model)
	cl := newLiveWorkflowDaemonWithWindow(t, baseURL, model, "", forcedOverflowNumCtx)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if win, src := servedContextWindow(t, cl); win != forcedOverflowNumCtx {
		t.Logf("WARNING daemon resolved context window %d (from %s), not the pinned %d",
			win, src, forcedOverflowNumCtx)
	} else {
		t.Logf("context window %d (from %s)", win, src)
	}

	guardOff := false
	events, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{
		Text:         forcedOverflowPrompt(),
		GuardEnabled: &guardOff,
	})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	s := drainWorkflowEvents(t, events)

	if s.errText == "" {
		t.Skipf("the run completed without overflowing (%d turns, %d tool calls) — %s did not emit a "+
			"tool call long enough to hit the %d-token ceiling; raise the requested line count or lower "+
			"the window to force it", len(s.turns), len(s.toolCalls), model, forcedOverflowNumCtx)
	}

	t.Logf("=== P62.5: run failed after %d turns with: %s", len(s.turns), s.errText)

	// The classification, which is the link under test. The drive's overflow
	// recovery keys on IsContextOverflowError, which reads this message — so an
	// error that reaches the user as the raw server text is one the drive will
	// treat as fatal instead of resetting and resuming.
	if !strings.Contains(s.errText, "response truncated at the context limit") {
		t.Errorf("the run failed at a context ceiling but the error was not classified as a truncation: %q\n"+
			"the phased drive's overflow ladder (P62.3) never fires on an error it cannot recognise, "+
			"so this is the whole recovery path silently disabled", s.errText)
	}
	// P35.2's other half: the actionable cause must be named, not just the
	// server's opaque parse failure.
	if !strings.Contains(s.errText, "provider.context_window") {
		t.Errorf("the truncation error does not name the lever that fixes it: %q", s.errText)
	}
}
