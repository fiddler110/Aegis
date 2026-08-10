package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/drive"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// mutatingDriveTools are the tools whose successful use counts as "this turn
// changed a file" for the P39.7 no-progress guard. Mirrors the CLI drive's
// list: a turn that only reads or narrates is what the guard exists to catch.
var mutatingDriveTools = map[string]bool{
	"write_file": true,
	"edit_file":  true,
	"multi_edit": true,
}

// driveSpec is the resolved, validated form of an api.DriveRequest: the phase
// plan has already been found, so the run cannot fail halfway in on "this skill
// isn't phased".
type driveSpec struct {
	skillName string
	skillDir  string
	task      string
	phases    []drive.Phase
	maxTurns  int
	// runDir resolves where this plan's file globs live — the skill's declared
	// `run_dir:`, the threat-model layout, or the workspace root. Resolved here
	// with the plan because the two are one decision: a plan judged against the
	// wrong directory has no working completion oracle.
	runDir func(cwd string) string
}

// driveRuntime carries the per-run objects streamRun already built, so the
// drive does not rebuild an engine, a workdir or an event path of its own.
type driveRuntime struct {
	eng     *engine.Engine
	system  string
	workdir string
	emit    engine.EmitFunc
	send    func(api.Event)

	iterToolCalls *int
	iterMutations *int
}

// handleDrive starts a phased skill drive in a session and streams it over the
// same SSE seam an ordinary turn uses (P52.12).
//
// Before this, every reliability mechanism built for local models — fresh
// context per phase, the hollow-body re-entry router, backend-liveness resume,
// context escalation, the no-progress guard — was reachable only through
// `aegis chat --skill`. The TUI and web UI ran the single-context drive that the
// phased drive exists *because* it fails, which is the wrong default for the
// clients most people use, and especially wrong for the web UI: a multi-hour
// build most wants to live somewhere that survives closing the terminal.
func (s *Server) handleDrive(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.DriveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Skill = strings.TrimSpace(req.Skill)
	req.Task = strings.TrimSpace(req.Task)
	if req.Skill == "" {
		writeError(w, http.StatusBadRequest, "skill is required")
		return
	}
	if req.Task == "" {
		writeError(w, http.StatusBadRequest, "task is required")
		return
	}

	spec, err := s.resolveDriveSpec(id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The drive's own first message is built per phase, so the shared body gets
	// the task as the turn's text purely for titling and task routing — the
	// conversation it seeds is discarded at the first phase boundary.
	s.streamRun(w, r, id, api.PostMessageRequest{
		Text:         req.Task,
		GuardEnabled: req.GuardEnabled,
		Resumable:    req.Resumable,
	}, spec)
}

// handleDriveSkills lists the skills this session can drive, so a UI can offer
// a drive control populated with real, workspace-resolved choices instead of a
// free-text skill name validated only after the request fails.
func (s *Server) handleDriveSkills(w http.ResponseWriter, r *http.Request) {
	workdir := s.workdirFor(r.PathValue("id"))
	out := api.DriveSkillsResponse{Skills: []api.DriveSkillInfo{}}
	for _, sk := range skills.Discover(workdir, s.cfg.DataDir, s.cfg.Skills.BuiltinEnabled) {
		phases := drive.PlanFor(sk.Name, sk.Phases)
		if len(phases) == 0 {
			continue
		}
		out.Skills = append(out.Skills, api.DriveSkillInfo{
			Name: sk.Name, Description: sk.Description, Phases: len(phases),
		})
	}
	// A skill named in the config's enabled list is discovered above, but a
	// dormant built-in with a plan is not — and threat-modeling is the one every
	// client is expected to be able to drive. Offer it unconditionally: the
	// drive itself enables it for the run (resolveDriveSpec appends the
	// requested name to the enabled list), so offering it here is not a promise
	// the request can't keep.
	if !containsDriveSkill(out.Skills, threatModelSkillName) {
		out.Skills = append(out.Skills, api.DriveSkillInfo{
			Name:        threatModelSkillName,
			Description: "STRIDE-A threat model of a repository or system",
			Phases:      len(drive.PlanFor(threatModelSkillName, nil)),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// threatModelSkillName is the one skill with a built-in plan, so it is drivable
// whether or not it has been enabled yet.
const threatModelSkillName = "threat-modeling"

func containsDriveSkill(list []api.DriveSkillInfo, name string) bool {
	for _, s := range list {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

// resolveDriveSpec finds the named skill and its phase plan, refusing the
// request when there is none. Refusing is deliberate: falling back to the
// single-context drive would give a caller that explicitly asked for a phased
// build the exact behavior the phased build exists to replace, and it would do
// so silently — the failure would show up hours later as a stalled run.
func (s *Server) resolveDriveSpec(sessionID string, req api.DriveRequest) (*driveSpec, error) {
	workdir := s.workdirFor(sessionID)
	enabled := appendUniqueStr(s.cfg.Skills.BuiltinEnabled, req.Skill)
	sk, ok := skills.Load(workdir, s.cfg.DataDir, enabled, req.Skill)
	if !ok || strings.TrimSpace(sk.Content) == "" {
		return nil, fmt.Errorf("skill %q not found or empty", req.Skill)
	}
	phases := drive.PlanFor(sk.Name, sk.Phases)
	if len(phases) == 0 {
		return nil, fmt.Errorf("skill %q has no phase plan; add a `phases:` list to its frontmatter, or send it as an ordinary message to use the single-context drive", req.Skill)
	}
	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultDriveMaxTurns
	}
	// P39.18: a skill that bundles scripts gets them as typed tools for this
	// session, rather than asking the model to compose `python …/scaffold.py
	// --framework <name>` as a string through the shell. Session-scoped for the
	// same reason on-demand skill activation is (activateSessionSkill): the
	// daemon-wide surface must not grow five schemas for every session that
	// never drives this skill.
	for _, t := range builtin.ThreatModelScriptTools(workdir, sk.Dir) {
		s.sessionToolRegistry(sessionID).Upsert(t)
	}
	return &driveSpec{
		skillName: sk.Name,
		skillDir:  sk.Dir,
		task:      req.Task,
		phases:    phases,
		maxTurns:  maxTurns,
		runDir:    drive.RunDirResolver(sk.Name, sk.RunDir),
	}, nil
}

// defaultDriveMaxTurns bounds each phase when the caller names no budget. It
// matches the CLI drive's `--max-turns` default: high enough that a phase on a
// slow local model is not cut off mid-authoring, low enough that a wedged phase
// hands back to the next one instead of running until the session cap.
const defaultDriveMaxTurns = 40

// runDrive executes the phase plan, translating the drive's operator notices
// into SSE notice events so a UI sees the same narration a terminal does.
func (s *Server) runDrive(ctx context.Context, spec *driveSpec, rt driveRuntime) error {
	notices := &noticeWriter{send: rt.send}
	st := &drive.State{
		Engine:     rt.eng,
		System:     rt.system,
		OnEvent:    rt.emit,
		Logger:     s.logger,
		ErrOut:     notices,
		Cwd:        rt.workdir,
		SkillName:  spec.skillName,
		SkillDir:   spec.skillDir,
		TaskPrompt: spec.task,
		MaxTurns:   spec.maxTurns,
		RunDir:     spec.runDir,
		// P47.5b: let a phase raise the serving window toward the model max on
		// a context overflow. Mutates the shared adapter, which P52.6 made safe
		// to call from a session goroutine — that synchronization is exactly
		// what this item was waiting on.
		EscalateWindow: s.driveWindowEscalator(),
		// P50.1: a liveness probe so the drive waits out a crashed or
		// restarting local model server and resumes from disk instead of
		// aborting. Reports unsupported for a cloud adapter, and the drive then
		// never waits on it.
		CheckBackend: func(hctx context.Context) (bool, bool) {
			return provider.CheckBackendHealth(hctx, s.adapter)
		},
		Progress:      &drive.Progress{},
		IterToolCalls: rt.iterToolCalls,
		IterMutations: rt.iterMutations,
	}
	rt.send(api.Event{Kind: api.KindNotice, Text: fmt.Sprintf(
		"driving %s in phased mode — one bounded fresh context per phase (%d content phases + verify)",
		spec.skillName, len(spec.phases))})
	return drive.Run(ctx, st, spec.phases)
}

// driveWindowEscalator builds the P47.5b escalation closure, or nil when this
// provider cannot escalate (a cloud adapter, or no detected model max) — the
// drive then relies on the fresh-context reset alone.
func (s *Server) driveWindowEscalator() func() (int, bool) {
	cur, _ := s.effectiveContextWindowFor(context.Background(), s.cfg.Provider.Model)
	max := s.modelContextMax(s.cfg.Provider.Model)
	if cur <= 0 || max <= 0 {
		return nil
	}
	return func() (int, bool) {
		next, grew := drive.NextWindow(cur, max)
		if !grew {
			return cur, false
		}
		if provider.RaiseContextWindow(s.adapter, next) {
			cur = next
			return next, true
		}
		return cur, false
	}
}

// noticeWriter turns the drive's `[notice: …]` lines into SSE notice events.
// The drive writes operator narration to an io.Writer because its first host
// was a terminal; here each non-empty line becomes one event, with the
// bracket-and-prefix decoration stripped so a UI can render it as a normal
// advisory rather than as terminal chrome.
type noticeWriter struct {
	send func(api.Event)
	buf  strings.Builder
}

func (n *noticeWriter) Write(p []byte) (int, error) {
	n.buf.Write(p)
	text := n.buf.String()
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			break
		}
		if line := cleanDriveNotice(text[:i]); line != "" {
			n.send(api.Event{Kind: api.KindNotice, Text: line})
		}
		text = text[i+1:]
	}
	n.buf.Reset()
	n.buf.WriteString(text)
	return len(p), nil
}

// cleanDriveNotice strips the `[notice: … ]` wrapper the CLI form uses.
func cleanDriveNotice(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "[notice: ")
	line = strings.TrimPrefix(line, "[warning: ")
	line = strings.TrimSuffix(line, "]")
	return strings.TrimSpace(line)
}

// appendUniqueStr appends v to list unless it is already present.
func appendUniqueStr(list []string, v string) []string {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return list
		}
	}
	return append(append([]string(nil), list...), v)
}
