package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/workspacetrust"
)

func permModeRank(mode string) int {
	switch mode {
	case "build":
		return 1
	case "auto":
		return 2
	default:
		return 0
	}
}

// resolveSessionMode picks the permission mode for a new session: an explicit
// reqMode always wins; otherwise a persona's Mode is used, except a loaded
// (non-built-in) persona — including one installed by a bundle (P5.7) or
// picked up from .aegis/personas/*.md — is less trusted than a built-in, so
// its Mode must not silently escalate a session past the configured default
// when the caller didn't explicitly ask for a mode (P7.5). Built-in personas
// are reviewed and shipped with Aegis, so they remain fully trusted. Returns
// "" when neither reqMode nor an applicable persona mode is set, leaving the
// caller to apply the configured default.
func (s *Server) resolveSessionMode(reqMode string, p persona.Persona) string {
	if reqMode != "" {
		return reqMode
	}
	if p.Mode == "" {
		return ""
	}
	if p.Loaded && permModeRank(p.Mode) > permModeRank(s.cfg.Permission.Mode) {
		s.logger.Warn("persona requested a more permissive mode than the configured default; ignoring",
			"persona", p.Name, "persona_mode", p.Mode, "default_mode", s.cfg.Permission.Mode)
		return ""
	}
	return p.Mode
}

// filterPersonaRules strips Allow rules contributed by a loaded (non-built-in)
// persona before they are merged into a session's rule set. A loaded persona
// is untrusted content (P7.5) in exactly the same way its Mode field is: an
// Allow rule short-circuits both the mode gate and the approver (RuleGate.Check),
// so an unfiltered "allow shell(*)" in a project-level persona.md would grant
// unattended access regardless of the configured plan/build/auto mode — a
// strictly bigger hole than the Mode escalation resolveSessionMode already
// blocks, since it bypasses mode entirely rather than just requesting a more
// permissive one. Deny rules only narrow access, so they carry none of that
// risk and pass through unchanged. Built-in personas (Loaded == false) are
// reviewed and shipped with Aegis, so their rules remain fully trusted.
func filterPersonaRules(rules []permission.Rule, p persona.Persona, logger *slog.Logger) []permission.Rule {
	if !p.Loaded {
		return rules
	}
	kept := make([]permission.Rule, 0, len(rules))
	for _, r := range rules {
		if r.Action == permission.RuleDeny {
			kept = append(kept, r)
			continue
		}
		if logger != nil {
			logger.Warn("ignoring persona allow rule from untrusted (loaded) persona", "persona", p.Name)
		}
	}
	return kept
}

// refreshPersonas rescans the persona directories so file edits, additions,
// and deletions take effect without a daemon restart. A directory signature
// makes this a cheap no-op when nothing changed, so persona-touching handlers
// call it on every request.
func (s *Server) refreshPersonas() {
	if n, changed := persona.Refresh(s.personaProjectDir, s.personaProjectTrusted, s.personaDirs...); changed {
		s.logger.Info("reloaded persona files", "count", n)
	}
}

// personaFor resolves a persona by name, scoped to root (P25.9): root == ""
// or the daemon's own workspace takes the fast, unchanged path (persona.Get,
// serving the shared Refresh-managed set); any other root additionally
// consults that root's own project persona directory via
// persona.GetForRoot's pure, non-mutating scan — see that function's doc
// comment for why a session-scoped persona.Refresh call would be unsafe.
func (s *Server) personaFor(root, name string) (persona.Persona, bool) {
	if root == "" || root == s.workspace {
		return persona.Get(name)
	}
	trusted := workspacetrust.Open(config.WorkspaceTrustStorePath()).IsTrusted(root)
	return persona.GetForRoot(root, trusted, name)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	workdir, werr := s.resolveSessionWorkdir(req.Workdir)
	if werr != nil {
		writeError(w, werr.status, werr.msg)
		return
	}
	s.refreshPersonas()
	p, _ := s.personaFor(workdir, req.Persona)
	mode := s.resolveSessionMode(req.Mode, p)
	if mode == "" {
		mode = s.cfg.Permission.Mode
	}
	if mode != "plan" && mode != "build" && mode != "auto" {
		writeError(w, http.StatusBadRequest, "mode must be plan, build, or auto")
		return
	}
	system := req.System
	if system == "" {
		system = p.System
	}
	sess, err := s.store.Create(r.Context(), req.Title, system, mode, req.Persona, workdir)
	if err != nil {
		s.logger.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if workdir != "" {
		s.sessionWorkdirs.Store(sess.ID, workdir)
	}
	writeJSON(w, http.StatusCreated, toMeta(session.Meta{ID: sess.ID, Title: sess.Title, Mode: sess.Mode, Workdir: sess.Workdir, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt}))
}

// workdirError pairs an HTTP status with a message for a rejected session
// Workdir request, so handleCreateSession can report a 400 (malformed/
// nonexistent path) or 403 (trust-boundary violation, P25.1) distinctly.
type workdirError struct {
	status int
	msg    string
}

func (e *workdirError) Error() string { return e.msg }

// resolveSessionWorkdir validates a client-supplied session Workdir (P25.1):
// empty keeps today's behavior (the daemon's default workspace, via
// workdirFor's fallback); otherwise it must resolve to an existing
// directory and, on a remote-accessible daemon, fall within the trust
// boundary workdirAllowed enforces.
func (s *Server) resolveSessionWorkdir(raw string) (string, *workdirError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", &workdirError{http.StatusBadRequest, "invalid workdir"}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", &workdirError{http.StatusBadRequest, "workdir does not exist or is not a directory"}
	}
	if !s.workdirAllowed(abs) {
		return "", &workdirError{http.StatusForbidden, "workdir is not permitted for a remote-accessible daemon; add it to server.session_workdir_allowlist"}
	}
	return abs, nil
}

func (s *Server) handleListTeammates(w http.ResponseWriter, _ *http.Request) {
	out := []api.Teammate{}
	if s.swarmReg != nil {
		for _, m := range s.swarmReg.List() {
			out = append(out, api.Teammate{
				AgentID:   m.Identity.AgentID,
				Name:      m.Identity.Name,
				Team:      m.Identity.Team,
				Status:    string(m.Status),
				Summary:   m.Summary,
				StartedAt: m.StartedAt,
				EndedAt:   m.EndedAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListRuns reports message runs currently in flight across all sessions,
// so concurrent user-driven parallel sessions are observable.
func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	out := []api.RunInfo{}
	if s.runs != nil {
		out = s.runs.list()
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListCronJobs reports persisted cron jobs so an operator can review
// what fires unattended — in particular which jobs carry auto_approve, since
// those bypass interactive approval at fire time (P27.15/FIND-08). This is
// the human-facing counterpart to the model-facing cron_list tool, reachable
// without going through an engine turn.
func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request) {
	out := []api.CronJobInfo{}
	if s.cronSched != nil {
		jobs, err := s.cronSched.List(r.Context())
		if err != nil {
			s.logger.Error("list cron jobs", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, j := range jobs {
			out = append(out, api.CronJobInfo{
				ID: j.ID, Schedule: j.Schedule, Command: j.Command, Title: j.Title,
				Enabled: j.Enabled, AutoApprove: j.AutoApprove, LastRun: j.LastRun,
				Created: j.Created, Workdir: j.Workdir,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	var (
		metas []session.Meta
		err   error
	)
	if r.URL.Query().Get("archived") == "true" {
		metas, err = s.store.ListAll(r.Context())
	} else {
		metas, err = s.store.List(r.Context())
	}
	if err != nil {
		s.logger.Error("list sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.SessionMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, toMeta(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if s.checkpoints != nil {
		if err := s.checkpoints.DeleteForSession(r.Context(), id); err != nil {
			s.logger.Warn("delete session checkpoints", "session", id, "err", err)
		}
	}
	s.sessionTools.Delete(id)
	s.sessionWorkdirs.Delete(id)
	s.sessionSkills.Delete(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.System == nil && req.Mode == nil && req.Persona == nil && req.Model == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	// A persona switch can arrive as the Persona field or as the legacy
	// "persona:<name>" System prefix; both take the same full-profile path so
	// the switch carries model, rules, and guard overrides — not just the
	// system prompt.
	personaName := ""
	if req.Persona != nil {
		personaName = strings.TrimSpace(*req.Persona)
		if personaName == "" {
			writeError(w, http.StatusBadRequest, "persona name is required")
			return
		}
	}
	if req.System != nil {
		if name, ok := strings.CutPrefix(*req.System, "persona:"); ok {
			personaName = name
			req.System = nil
		}
	}
	if personaName != "" {
		s.refreshPersonas()
		p, found := s.personaFor(s.workdirFor(id), personaName)
		if !found {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown persona %q", personaName))
			return
		}
		if err := s.store.SetPersona(r.Context(), id, p.Name); err != nil {
			s.logger.Error("set persona", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if req.System == nil {
			req.System = &p.System
		}
		// Apply the persona's permission mode (subject to the P7.5 escalation
		// guard) unless the request pins a mode explicitly.
		if req.Mode == nil {
			if m := s.resolveSessionMode("", p); m != "" {
				req.Mode = &m
			}
		}
	}

	if req.System != nil {
		if err := s.store.SetSystem(r.Context(), id, *req.System); err != nil {
			s.logger.Error("set system", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if req.Mode != nil {
		m := *req.Mode
		if m != "plan" && m != "build" && m != "auto" {
			writeError(w, http.StatusBadRequest, "mode must be plan, build, or auto")
			return
		}
		if err := s.store.SetMode(r.Context(), id, m); err != nil {
			s.logger.Error("set mode", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	// P14.7: a per-session model override. Not validated against the
	// configured provider's actual model list (same posture as a persona's
	// own Model field) — an unrecognized id surfaces as a provider error on
	// the next turn rather than at switch time.
	if req.Model != nil {
		if err := s.store.SetModel(r.Context(), id, strings.TrimSpace(*req.Model)); err != nil {
			s.logger.Error("set model", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMeta(session.Meta{ID: sess.ID, Title: sess.Title, Mode: sess.Mode, Model: sess.Model, InputTokens: sess.InputTokens, OutputTokens: sess.OutputTokens, CostUSD: sess.CostUSD, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt}))
}

// handleActivateSkill turns on a dormant embedded built-in skill for this
// session only (P22.x): unlike /skills enable (which writes config and needs
// a restart), this takes effect on the very next turn, and never persists —
// a fresh session starts with every built-in dormant again.
func (s *Server) handleActivateSkill(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.ActivateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !skills.IsBuiltin(name) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown built-in skill %q", name))
		return
	}
	if _, err := s.store.Get(r.Context(), id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.activateSessionSkill(id, name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListCommands(w http.ResponseWriter, _ *http.Request) {
	var out []api.CommandInfo
	if s.cmdReg != nil {
		for _, c := range s.cmdReg.List() {
			out = append(out, api.CommandInfo{Name: c.Name, Description: c.Description, Args: c.Args})
		}
	}
	if out == nil {
		out = []api.CommandInfo{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetMemory(w http.ResponseWriter, _ *http.Request) {
	resp := api.MemoryResponse{
		ProjectMemory: readIfExists(s.memory.ProjectMemoryPath()),
		UserMemory:    readIfExists(s.memory.GlobalMemoryPath()),
	}
	for _, sk := range skills.Discover(s.workspace, s.cfg.DataDir, s.cfg.Skills.BuiltinEnabled) {
		resp.Skills = append(resp.Skills, sk.Name)
	}
	if resp.Skills == nil {
		resp.Skills = []string{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAppendMemory(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.AppendMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Entry) == "" {
		writeError(w, http.StatusBadRequest, "entry is required")
		return
	}
	path := s.memory.ProjectMemoryPath()
	if req.Scope == "user" {
		path = s.memory.GlobalMemoryPath()
	}
	if err := memory.Append(path, req.Entry); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPersonas(w http.ResponseWriter, _ *http.Request) {
	s.refreshPersonas()
	names := persona.Names()
	out := make([]api.PersonaInfo, 0, len(names))
	for _, name := range names {
		p, _ := persona.Get(name)
		out = append(out, api.PersonaInfo{Name: p.Name, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *Server) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	if s.checkpoints == nil {
		writeJSON(w, http.StatusOK, []api.CheckpointInfo{})
		return
	}
	cps, err := s.checkpoints.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.logger.Error("list checkpoints", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.CheckpointInfo, 0, len(cps))
	for _, cp := range cps {
		out = append(out, api.CheckpointInfo{
			ID:        cp.ID,
			Seq:       cp.Seq,
			Label:     cp.Label,
			GitSHA:    cp.GitSHA,
			FileCount: cp.FileCount,
			CreatedAt: cp.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRewind restores a session to a checkpoint. scope selects what to
// restore: "code" (files only), "conversation" (messages only), or "both"
// (default).
func (s *Server) handleRewind(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if s.checkpoints == nil {
		writeError(w, http.StatusServiceUnavailable, "checkpointing not available")
		return
	}
	id := r.PathValue("id")
	var req api.RewindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.CheckpointID == "" {
		writeError(w, http.StatusBadRequest, "checkpoint_id is required")
		return
	}

	// Serialize against an in-flight run on this session (same semaphore
	// handlePostMessage acquires): without this, a turn that's already running
	// finishes after the truncation below and appends its tail using a
	// Persisted offset captured before the rewind, silently reviving content
	// the user just rewound away. Waiting here is safe — the alternative queue
	// order (run first, then rewind) is exactly what handlePostMessage already
	// imposes on a second concurrent request.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

	scope := req.Scope
	if scope == "" {
		scope = "both"
	}
	if scope != "both" && scope != "code" && scope != "conversation" {
		writeError(w, http.StatusBadRequest, "scope must be both, code, or conversation")
		return
	}

	cp, err := s.checkpoints.Get(r.Context(), req.CheckpointID)
	if err != nil {
		if errors.Is(err, checkpoint.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checkpoint not found")
			return
		}
		s.logger.Error("get checkpoint", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cp.SessionID != id {
		writeError(w, http.StatusBadRequest, "checkpoint does not belong to this session")
		return
	}

	resp := api.RewindResponse{Scope: scope}

	if scope == "both" || scope == "code" {
		// P3.4: git-native rollback — run `git reset --hard <sha>` before restoring
		// snapshotted files so untracked changes and index state are also reset.
		if req.GitRollback && cp.GitSHA != "" {
			gitArgs := []string{"-C", s.workdirFor(id), "reset", "--hard", cp.GitSHA}
			if out, err := execGitCmd(r.Context(), gitArgs...); err != nil {
				s.logger.Warn("git rollback failed", "sha", cp.GitSHA, "out", out, "err", err)
			} else {
				s.logger.Info("git rollback", "sha", cp.GitSHA)
			}
		}
		n, err := s.checkpoints.RestoreFiles(r.Context(), cp.ID)
		if err != nil {
			s.logger.Warn("rewind: restore files", "checkpoint", cp.ID, "err", err)
		}
		resp.FilesRestored = n
		// Clear file-staleness tracking: we rewrote files out of band, so the
		// agent must re-read them rather than be blocked by a stale-mtime guard.
		if s.fileTracker != nil {
			s.fileTracker.Clear()
		}
	}

	if scope == "both" || scope == "conversation" {
		sess, err := s.store.Get(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		keep := cp.Seq
		if keep < 0 {
			keep = 0
		}
		if keep > len(sess.Messages) {
			keep = len(sess.Messages)
		}
		if err := s.store.SaveMessages(r.Context(), id, sess.Messages[:keep]); err != nil {
			s.logger.Error("rewind: save truncated messages", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		resp.MessagesKept = keep
	} else if sess, err := s.store.Get(r.Context(), id); err == nil {
		resp.MessagesKept = len(sess.Messages)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleFork creates a new session that starts as a copy of an existing
// session's conversation (P22.3) — the non-destructive counterpart to
// /rewind. An optional checkpoint id truncates the new session's messages to
// that checkpoint's Seq, same as rewind's "conversation" scope; omitted, the
// fork carries the full current conversation. Either way the source session
// is read-only here: it is never truncated or otherwise mutated, so a risky
// edit-and-retry can never damage the original.
func (s *Server) handleFork(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.ForkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// Same in-flight-run serialization handleRewind uses: without it, a turn
	// still appending its tail could interleave with the sess.Messages read
	// below and get a torn/partial snapshot copied into the fork.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	keep := len(sess.Messages)
	if req.CheckpointID != "" {
		if s.checkpoints == nil {
			writeError(w, http.StatusServiceUnavailable, "checkpointing not available")
			return
		}
		cp, err := s.checkpoints.Get(r.Context(), req.CheckpointID)
		if err != nil {
			if errors.Is(err, checkpoint.ErrNotFound) {
				writeError(w, http.StatusNotFound, "checkpoint not found")
				return
			}
			s.logger.Error("get checkpoint", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if cp.SessionID != id {
			writeError(w, http.StatusBadRequest, "checkpoint does not belong to this session")
			return
		}
		keep = cp.Seq
		if keep < 0 {
			keep = 0
		}
		if keep > len(sess.Messages) {
			keep = len(sess.Messages)
		}
	}

	title := forkTitle(sess.Title)
	forked, err := s.store.Create(r.Context(), title, sess.System, sess.Mode, sess.Persona, sess.Workdir)
	if err != nil {
		s.logger.Error("fork: create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sess.Model != "" {
		// Carry a per-session model override (P14.7) forward; SetModel is a
		// second write rather than a Create param since Create predates it and
		// only one caller (this one) needs it.
		if err := s.store.SetModel(r.Context(), forked.ID, sess.Model); err != nil {
			s.logger.Warn("fork: carry model override", "session", forked.ID, "err", err)
		}
	}
	if err := s.store.SaveMessages(r.Context(), forked.ID, sess.Messages[:keep]); err != nil {
		s.logger.Error("fork: save messages", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, api.ForkResponse{SessionID: forked.ID, Title: title, MessagesKept: keep})
}

// forkTitle derives a forked session's title from its source's, mirroring
// git's "branch off" naming convention (no dedicated model call — this stays
// a cheap, deterministic label, unlike generateTitle's async first-message
// summarization for a brand-new top-level session).
func forkTitle(orig string) string {
	if orig == "" {
		return "Fork"
	}
	return "Fork of " + orig
}

// handleCompactSession forces context compaction on a session's current
// message history outside of a model turn (P19.2) — e.g. before a long
// tool-heavy stretch the user knows is coming, rather than waiting for the
// automatic budget-driven trigger in engine.Run.
func (s *Server) handleCompactSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	summarizer, ok := s.compactor.(*compaction.Summarizer)
	if !ok || summarizer == nil {
		writeError(w, http.StatusServiceUnavailable, "compaction not available (no model adapter configured)")
		return
	}

	// Serialize against an in-flight run on this session, same as rewind.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	before := len(sess.Messages)
	out, changed, err := summarizer.ForceCompact(r.Context(), sess.System, sess.Messages)
	if err != nil {
		s.logger.Warn("manual compaction failed", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("compaction failed: %v", err))
		return
	}
	if changed {
		if err := s.store.SaveMessages(r.Context(), id, out); err != nil {
			s.logger.Error("save compacted messages", "session", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	writeJSON(w, http.StatusOK, api.CompactResponse{
		Compacted:      changed,
		MessagesBefore: before,
		MessagesAfter:  len(out),
	})
}

// handleSetBackground marks or unmarks a session as a background (detached)
// session. A background session's engine runs are detached from the HTTP
// request context so the turn continues even if the TUI disconnects (P3.2).
func (s *Server) handleSetBackground(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.SetBackgroundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.store.SetBackground(r.Context(), id, req.Background); err != nil {
		s.logger.Error("set background", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetBGEvents returns buffered engine events for a background session,
// supporting incremental polling via the ?since=<id> query parameter (P3.2).
func (s *Server) handleGetBGEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		fmt.Sscan(v, &since)
	}
	events, err := s.store.ListBGEvents(r.Context(), id, since)
	if err != nil {
		s.logger.Error("list bg events", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.BGEventItem, 0, len(events))
	for _, e := range events {
		out = append(out, api.BGEventItem{ID: e.ID, Data: e.Data})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleArchiveSession soft-deletes a session; it is hidden from normal listings.
func (s *Server) handleArchiveSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Archive(r.Context(), id); err != nil {
		s.logger.Error("archive session", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnarchiveSession restores an archived session to active status.
func (s *Server) handleUnarchiveSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Unarchive(r.Context(), id); err != nil {
		s.logger.Error("unarchive session", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePruneSessions deletes non-archived sessions older than the configured TTL,
// or an explicit ?days=N override from the request.
func (s *Server) handlePruneSessions(w http.ResponseWriter, r *http.Request) {
	days := s.cfg.Cleanup.SessionTTLDays
	if v := r.URL.Query().Get("days"); v != "" {
		fmt.Sscan(v, &days)
	}
	if days <= 0 {
		writeError(w, http.StatusBadRequest, "days must be > 0 (or configure cleanup.session_ttl_days)")
		return
	}
	n, err := s.store.Prune(r.Context(), time.Duration(days)*24*time.Hour)
	if err != nil {
		s.logger.Error("prune sessions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("pruned old sessions", "deleted", n, "ttl_days", days)
	writeJSON(w, http.StatusOK, api.PruneResponse{Deleted: n})
}

// execGitCmd runs a git sub-command and returns combined output. Used for
// git-native rollback (P3.4).
func execGitCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// captureGitSHA returns the current HEAD commit SHA via `git rev-parse HEAD`,
// returning an empty string if git is unavailable or the directory is not a
// repo.
func captureGitSHA(ctx context.Context, root string) string {
	out, err := execGitCmd(ctx, "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func toMeta(m session.Meta) api.SessionMeta {
	return api.SessionMeta{
		ID:           m.ID,
		Title:        m.Title,
		Mode:         m.Mode,
		Workdir:      m.Workdir,
		Model:        m.Model,
		Background:   m.Background,
		Archived:     m.Archived,
		ArchivedAt:   m.ArchivedAt,
		InputTokens:  m.InputTokens,
		OutputTokens: m.OutputTokens,
		CostUSD:      m.CostUSD,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func deriveTitle(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) > 60 {
		text = string(runes[:60]) + "…"
	}
	return text
}

// generateTitle calls the model asynchronously to produce a short session
// title from the user's first message. Falls back to deriveTitle when no
// SmallModel is configured (avoids a full-model call just for a title).
func (s *Server) generateTitle(sessionID, firstMessage string) {
	model := s.cfg.Provider.SmallModel
	if model == "" || s.adapter == nil {
		// No dedicated small model configured; use the simple truncation fallback.
		_ = s.store.SetTitle(context.Background(), sessionID, deriveTitle(firstMessage))
		return
	}

	prompt := "Give a short title (max 8 words, no punctuation) for a chat that started with:\n" + firstMessage
	req := provider.Request{
		Model:     model,
		MaxTokens: 48,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := s.adapter.Stream(ctx, req)
	if err != nil {
		_ = s.store.SetTitle(context.Background(), sessionID, deriveTitle(firstMessage))
		return
	}

	var sb strings.Builder
	for ev := range ch {
		if ev.Type == provider.EventTextDelta {
			sb.WriteString(ev.Text)
		}
	}
	title := cleanTitle(strings.TrimSpace(sb.String()))
	if title == "" {
		title = deriveTitle(firstMessage)
	}
	_ = s.store.SetTitle(context.Background(), sessionID, title)
}

// cleanTitle strips thinking tags and trims whitespace from a model-generated title.
func cleanTitle(s string) string {
	// Remove <think>...</think> blocks produced by reasoning models.
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			s = strings.TrimSpace(s[:start])
			break
		}
		s = strings.TrimSpace(s[:start] + s[start+end+len("</think>"):])
	}
	// Collapse internal whitespace and trim surrounding quotes.
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, `"'`)
	runes := []rune(s)
	if len(runes) > 70 {
		s = string(runes[:70]) + "…"
	}
	return s
}
