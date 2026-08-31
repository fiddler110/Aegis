package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/tool"
)

// CronTools returns the cron-job management tools, all backed by the same
// cron.Scheduler. They let the model schedule recurring shell commands.
func CronTools(sched *cron.Scheduler) []tool.Tool {
	return []tool.Tool{
		&cronCreateTool{sched: sched},
		&cronListTool{sched: sched},
		&cronDeleteTool{sched: sched},
		&cronToggleTool{sched: sched},
		&cronHistoryTool{sched: sched},
	}
}

// --- cron_create ---

type cronCreateTool struct{ sched *cron.Scheduler }

func (t *cronCreateTool) Name() string                { return "cron_create" }
func (t *cronCreateTool) Capability() tool.Capability { return tool.CapExecute }
func (t *cronCreateTool) Description() string {
	return "Create a recurring cron job. The schedule is a standard 5-field cron expression " +
		"(minute hour dom month dow) or a macro (@hourly, @daily, @weekly, @monthly). " +
		"The command runs as a background shell job each time it fires. Because no one is " +
		"present to approve the run when it fires unattended, the job is blocked at fire time " +
		"unless the daemon's current permission mode is auto, or auto_approve is set here. " +
		"Set notify to have each fire's outcome and output delivered out-of-band (desktop " +
		"notification and/or webhook, per the user's notify config) instead of only being " +
		"readable via cron_history — use it for jobs whose whole purpose is to tell the user " +
		"something, such as a daily digest or a watch job."
}
func (t *cronCreateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"schedule":{"type":"string","description":"5-field cron expression or @hourly/@daily/@weekly/@monthly"},"command":{"type":"string","description":"shell command to run on each tick"},"title":{"type":"string","description":"optional short label for the job"},"auto_approve":{"type":"boolean","description":"allow this job to fire unattended even in build mode, where shell execution would otherwise require interactive approval (default false)"},"notify":{"type":"boolean","description":"deliver each fire's status and output over the configured notification channels (default false). Requires notify.desktop or notify.webhook to be configured; inert otherwise."}},"required":["schedule","command"]}`)
}
func (t *cronCreateTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Schedule    string `json:"schedule"`
		Command     string `json:"command"`
		Title       string `json:"title"`
		AutoApprove bool   `json:"auto_approve"`
		Notify      bool   `json:"notify"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Schedule) == "" {
		return tool.Result{Content: "schedule is required", IsError: true}, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return tool.Result{Content: "command is required", IsError: true}, nil
	}
	// Captured from the calling turn's workdir (P25.8) so a job created from
	// a session rooted outside the daemon's own workspace fires there too,
	// instead of always running in the daemon's cwd regardless of which
	// session scheduled it.
	workdir, _ := tool.WorkdirFromContext(ctx)
	j, err := t.sched.Create(ctx, args.Schedule, args.Command, args.Title, args.AutoApprove, workdir, args.Notify)
	if err != nil {
		return tool.Result{Content: "cron_create: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("Created cron job %s (id %s), schedule %q. Manage with cron_list, cron_toggle, cron_delete.", j.Title, j.ID, j.Schedule)}, nil
}

// --- cron_list ---

type cronListTool struct{ sched *cron.Scheduler }

func (t *cronListTool) Name() string                { return "cron_list" }
func (t *cronListTool) Capability() tool.Capability { return tool.CapRead }
func (t *cronListTool) Description() string {
	return "List all cron jobs with their id, schedule, enabled state, and title."
}
func (t *cronListTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{}}`)
}
func (t *cronListTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	jobs, err := t.sched.List(ctx)
	if err != nil {
		return tool.Result{Content: "cron_list: " + err.Error(), IsError: true}, nil
	}
	if len(jobs) == 0 {
		return tool.Result{Content: "(no cron jobs)"}, nil
	}
	var sb strings.Builder
	for _, j := range jobs {
		enabled := "enabled"
		if !j.Enabled {
			enabled = "disabled"
		}
		approve := ""
		if j.AutoApprove {
			approve = " [auto_approve]"
		}
		if j.Notify {
			approve += " [notify]"
		}
		fmt.Fprintf(&sb, "%s  %-10s  %-14s  %s  %s%s\n", j.ID, enabled, j.Schedule, j.Command, j.Title, approve)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// --- cron_delete ---

type cronDeleteTool struct{ sched *cron.Scheduler }

func (t *cronDeleteTool) Name() string { return "cron_delete" }

// Capability is CapExecute, not the CapWrite the row-in-a-database reading
// would suggest (DR-3). The question a capability answers is how much authority
// the call needs, and the object being written here is a scheduler entry that
// runs commands unattended: deleting one silently retires an operator's
// scheduled security scan, backup or audit job. CapWrite is allowed *silently*
// in build mode, so a prompt-injected model could do that with no prompt and no
// record beyond a tool trace, while cron_create — the inverse operation —
// correctly costs an approval.
func (t *cronDeleteTool) Capability() tool.Capability { return tool.CapExecute }
func (t *cronDeleteTool) Description() string {
	return "Delete a cron job by id."
}
func (t *cronDeleteTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the cron job id"}},"required":["id"]}`)
}
func (t *cronDeleteTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.ID == "" {
		return tool.Result{Content: "id is required", IsError: true}, nil
	}
	if err := t.sched.Delete(ctx, args.ID); err != nil {
		return tool.Result{Content: "cron_delete: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: "deleted " + args.ID}, nil
}

// --- cron_toggle ---

type cronToggleTool struct{ sched *cron.Scheduler }

func (t *cronToggleTool) Name() string { return "cron_toggle" }

// Capability is CapExecute for cron_delete's reason (DR-3), and the enable
// direction makes the case sharper still: re-enabling a job the operator
// deliberately switched off is a privilege-*restoring* operation, and a job
// created earlier with auto_approve runs unattended once it is back on.
func (t *cronToggleTool) Capability() tool.Capability { return tool.CapExecute }
func (t *cronToggleTool) Description() string {
	return "Toggle a cron job's enabled/disabled state by id."
}
func (t *cronToggleTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"the cron job id"}},"required":["id"]}`)
}
func (t *cronToggleTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.ID == "" {
		return tool.Result{Content: "id is required", IsError: true}, nil
	}
	nowEnabled, err := t.sched.Toggle(ctx, args.ID)
	if err != nil {
		return tool.Result{Content: "cron_toggle: " + err.Error(), IsError: true}, nil
	}
	state := "enabled"
	if !nowEnabled {
		state = "disabled"
	}
	return tool.Result{Content: fmt.Sprintf("cron job %s is now %s", args.ID, state)}, nil
}

// --- cron_history ---

type cronHistoryTool struct{ sched *cron.Scheduler }

func (t *cronHistoryTool) Name() string                { return "cron_history" }
func (t *cronHistoryTool) Capability() tool.Capability { return tool.CapRead }
func (t *cronHistoryTool) Description() string {
	return "List cron job fire-attempt audit history: job id, fired-at time, exit status " +
		"(ok/error/blocked), and a truncated snippet of the run's combined output. Most recent " +
		"first. Optionally filter to a single job id and/or cap the number of rows returned " +
		"(default 20)."
}

// ShortDescription is the deferred-tools advertisement (P62.6): the derived
// fallback cuts mid-clause, because this Description's first sentence spends
// most of itself enumerating columns.
func (t *cronHistoryTool) ShortDescription() string {
	return "List a cron job's fire-attempt history: time, exit status, and an output snippet."
}
func (t *cronHistoryTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"id":{"type":"string","description":"optional cron job id to filter history to"},"limit":{"type":"integer","description":"optional max number of run records to return (default 20)"}}}`)
}
func (t *cronHistoryTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	runs, err := t.sched.History(ctx, args.ID, limit)
	if err != nil {
		return tool.Result{Content: "cron_history: " + err.Error(), IsError: true}, nil
	}
	if len(runs) == 0 {
		return tool.Result{Content: "(no cron run history)"}, nil
	}
	var sb strings.Builder
	for _, r := range runs {
		snippet := strings.ReplaceAll(r.Output, "\n", " ")
		if len(snippet) > 120 {
			snippet = snippet[:120] + "..."
		}
		fmt.Fprintf(&sb, "%s  %-7s  job=%s  %s\n", r.FiredAt.Format(time.RFC3339), r.Status, r.JobID, snippet)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}
