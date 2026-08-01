package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// phaseWritingAdapter stands in for a model that does the phase's work: each
// turn it clears one file's `<!-- PENDING -->` marker, which is exactly the
// signal the drive's completion oracle reads. Writing directly rather than
// through a tool call keeps the test about the drive's phase machinery — tool
// dispatch is the engine's job and is covered elsewhere.
type phaseWritingAdapter struct {
	mu    sync.Mutex
	files []string // cleared in order, one per turn
	turns int
}

func (*phaseWritingAdapter) Name() string { return "phase-writing" }

func (a *phaseWritingAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	if a.turns < len(a.files) {
		_ = os.WriteFile(a.files[a.turns], []byte("# done\n\nreal content\n"), 0o644)
	}
	a.turns++
	a.mu.Unlock()

	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "phase turn"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 3, OutputTokens: 1}}
	close(ch)
	return ch, nil
}

func (a *phaseWritingAdapter) turnCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turns
}

// writeSkill writes a project skill file into workspace/.aegis/skills.
func writeSkill(t *testing.T, workspace, name, body string) {
	t.Helper()
	dir := filepath.Join(workspace, ".aegis", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newDriveTestServer starts a daemon rooted at workspace so session workdirs,
// skill discovery, and the drive's run directory all agree.
func newDriveTestServer(t *testing.T, workspace string, adapter provider.Adapter) *client.Client {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, adapter, tool.NewRegistry())
	srv.workspace = workspace
	srv.authToken = "test-token"
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token")
}

const twoPhaseSkill = `---
name: two-phase
description: a test skill with a declared phase plan
phases:
  - name: outline
    setup: true
    files: ["outline.md"]
    prompt: "Draft the outline for {task}."
  - name: chapters
    files: ["ch-1.md"]
    prompt: "Write chapter one."
---

Body of the test skill.
`

// TestDriveRunsDeclaredPhases is the end-to-end P52.12 regression: a skill that
// declares `phases:` in its own frontmatter is driven phase-by-phase over the
// daemon's SSE seam, with no code in the daemon naming that skill. It covers
// both halves of the generalization — the plan is read from frontmatter, and
// the completion oracle resolves that plan's files (here, against the workspace
// root, since the skill declares no run_dir).
func TestDriveRunsDeclaredPhases(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "two-phase", twoPhaseSkill)
	// Pre-scaffold both files as PENDING, standing in for the setup step's
	// stub-first pattern: the marker is what makes a phase incomplete.
	outline := filepath.Join(ws, "outline.md")
	chapter := filepath.Join(ws, "ch-1.md")
	for _, f := range []string{outline, chapter} {
		if err := os.WriteFile(f, []byte("<!-- PENDING: body -->\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &phaseWritingAdapter{files: []string{outline, chapter}}
	cl := newDriveTestServer(t, ws, adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cl.Drive(ctx, meta.ID, api.DriveRequest{Skill: "two-phase", Task: "write the doc"})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	var notices []string
	for ev := range ch {
		switch ev.Kind {
		case api.KindNotice:
			notices = append(notices, ev.Text)
		case api.KindError:
			t.Fatalf("drive reported an error event: %s", ev.Error)
		}
	}

	if got := adapter.turnCount(); got != 2 {
		t.Errorf("model turns = %d, want 2 (one per phase)", got)
	}
	for _, f := range []string{outline, chapter} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "<!-- PENDING") {
			t.Errorf("%s still carries a PENDING marker after the drive", filepath.Base(f))
		}
	}

	joined := strings.Join(notices, "\n")
	for _, want := range []string{"driving two-phase in phased mode", "phase 1/2 — outline", "phase 2/2 — chapters"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a notice containing %q; got:\n%s", want, joined)
		}
	}
	for _, n := range notices {
		if strings.HasPrefix(n, "[notice:") || strings.HasSuffix(n, "]") {
			t.Errorf("notice still carries terminal decoration: %q", n)
		}
	}
}

// TestDriveSkipsPhasesAlreadyCompleteOnDisk covers resume-from-disk at the
// daemon seam: a phase whose files carry no PENDING marker costs no model turn.
// This is what makes an interrupted multi-hour build cheap to restart, and it
// is the behaviour a client re-issuing a drive after a disconnect depends on.
func TestDriveSkipsPhasesAlreadyCompleteOnDisk(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "two-phase", twoPhaseSkill)
	if err := os.WriteFile(filepath.Join(ws, "outline.md"), []byte("# done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chapter := filepath.Join(ws, "ch-1.md")
	if err := os.WriteFile(chapter, []byte("<!-- PENDING: body -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &phaseWritingAdapter{files: []string{chapter}}
	cl := newDriveTestServer(t, ws, adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cl.Drive(ctx, meta.ID, api.DriveRequest{Skill: "two-phase", Task: "write the doc"})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	var notices []string
	for ev := range ch {
		if ev.Kind == api.KindNotice {
			notices = append(notices, ev.Text)
		}
	}
	if got := adapter.turnCount(); got != 1 {
		t.Errorf("model turns = %d, want 1 — the completed phase must cost nothing", got)
	}
	if !strings.Contains(strings.Join(notices, "\n"), "already complete on disk") {
		t.Errorf("expected a skip notice; got:\n%s", strings.Join(notices, "\n"))
	}
}

// TestDriveRefusesSkillWithoutPhasePlan: a drive request for a skill with no
// phase plan is refused rather than quietly run as one growing conversation.
// Falling back would hand a caller that explicitly asked for a phased build the
// exact failure the phased build exists to replace — and silently, since the
// symptom (a stalled single-context run) only shows up hours later.
func TestDriveRefusesSkillWithoutPhasePlan(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "flat", "---\nname: flat\ndescription: no phases here\n---\n\nBody.\n")
	cl := newDriveTestServer(t, ws, &phaseWritingAdapter{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cl.Drive(ctx, meta.ID, api.DriveRequest{Skill: "flat", Task: "build it"})
	if err == nil {
		t.Fatal("expected a drive for a skill with no phase plan to be refused")
	}
	if !strings.Contains(err.Error(), "phases:") {
		t.Errorf("expected the error to say how to opt in; got: %v", err)
	}
}

// TestDriveValidatesRequest covers the request-level refusals: an unknown skill
// and each missing required field.
func TestDriveValidatesRequest(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "two-phase", twoPhaseSkill)
	cl := newDriveTestServer(t, ws, &phaseWritingAdapter{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  api.DriveRequest
		want string
	}{
		{"no skill", api.DriveRequest{Task: "build it"}, "skill is required"},
		{"no task", api.DriveRequest{Skill: "two-phase"}, "task is required"},
		{"blank task", api.DriveRequest{Skill: "two-phase", Task: "   "}, "task is required"},
		{"unknown skill", api.DriveRequest{Skill: "nope", Task: "build it"}, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cl.Drive(ctx, meta.ID, tc.req); err == nil {
				t.Fatal("expected the request to be refused")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestDriveSkillsListsOnlyPhasedSkills: the listing a drive control populates
// itself from must include project-local phased skills, exclude skills with no
// plan (driving one is refused, so offering it would only produce an error),
// and always offer threat-modeling, whose plan is built in and whose enablement
// the drive handles for itself.
func TestDriveSkillsListsOnlyPhasedSkills(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "two-phase", twoPhaseSkill)
	writeSkill(t, ws, "flat", "---\nname: flat\ndescription: no phases here\n---\n\nBody.\n")
	cl := newDriveTestServer(t, ws, &phaseWritingAdapter{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build", Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	list, err := cl.DriveSkills(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]api.DriveSkillInfo{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if _, ok := byName["flat"]; ok {
		t.Error("a skill with no phase plan must not be offered as drivable")
	}
	tp, ok := byName["two-phase"]
	if !ok {
		t.Fatalf("expected the project skill to be listed; got %+v", list)
	}
	if tp.Phases != 2 {
		t.Errorf("two-phase phases = %d, want 2", tp.Phases)
	}
	if tp.Description == "" {
		t.Error("expected the skill's frontmatter description to be carried through")
	}
	if tm, ok := byName["threat-modeling"]; !ok || tm.Phases == 0 {
		t.Errorf("expected threat-modeling to always be drivable; got %+v", byName["threat-modeling"])
	}
}

// TestNoticeWriterEmitsCleanLines: the drive narrates to an io.Writer because
// its first host was a terminal. Each complete line must become exactly one SSE
// notice with the terminal decoration stripped, and a partial write must be
// held until its newline arrives rather than emitted as a fragment.
func TestNoticeWriterEmitsCleanLines(t *testing.T) {
	var got []string
	nw := &noticeWriter{send: func(ev api.Event) {
		if ev.Kind != api.KindNotice {
			t.Errorf("expected a notice event, got kind %q", ev.Kind)
		}
		got = append(got, ev.Text)
	}}

	nw.Write([]byte("\n[notice: phase 1/2 — outline]\n"))
	nw.Write([]byte("[warning: model stalled]\n\n"))
	nw.Write([]byte("partial line, no newline yet"))

	want := []string{"phase 1/2 — outline", "model stalled"}
	if len(got) != len(want) {
		t.Fatalf("got %d notices %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("notice %d = %q, want %q", i, got[i], want[i])
		}
	}

	nw.Write([]byte(" — now complete]\n"))
	if len(got) != 3 || !strings.Contains(got[2], "partial line, no newline yet — now complete") {
		t.Errorf("expected the buffered fragment to be emitted once its line closed; got %q", got)
	}
}
