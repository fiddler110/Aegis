package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scottymacleod/aegis/internal/cron"
	"github.com/scottymacleod/aegis/internal/tool"
)

// CronTools returns the cron-job management tools, all backed by the same
// cron.Scheduler. They let the model schedule recurring shell commands.
func CronTools(sched *cron.Scheduler) []tool.Tool {
	return []tool.Tool{
		&cronCreateTool{sched: sched},
		&cronListTool{sched: sched},
		&cronDeleteTool{sched: sched},
		&cronToggleTool{sched: sched},
	}
}

// --- cron_create ---

type cronCreateTool struct{ sched *cron.Scheduler }

func (t *cronCreateTool) Name() string                { return "cron_create" }
func (t *cronCreateTool) Capability() tool.Capability { return tool.CapExecute }
func (t *cronCreateTool) Description() string {
	return "Create a recurring cron job. The schedule is a standard 5-field cron expression " +
		"(minute hour dom month dow) or a macro (@hourly, @daily, @weekly, @monthly). " +
		"The command runs as a background shell job each time it fires."
}
func (t *cronCreateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"schedule":{"type":"string","description":"5-field cron expression or @hourly/@daily/@weekly/@monthly"},"command":{"type":"string","description":"shell command to run on each tick"},"title":{"type":"string","description":"optional short label for the job"}},"required":["schedule","command"]}`)
}
func (t *cronCreateTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Title    string `json:"title"`
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
	j, err := t.sched.Create(ctx, args.Schedule, args.Command, args.Title)
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
		fmt.Fprintf(&sb, "%s  %-10s  %-14s  %s  %s\n", j.ID, enabled, j.Schedule, j.Command, j.Title)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// --- cron_delete ---

type cronDeleteTool struct{ sched *cron.Scheduler }

func (t *cronDeleteTool) Name() string                { return "cron_delete" }
func (t *cronDeleteTool) Capability() tool.Capability { return tool.CapWrite }
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

func (t *cronToggleTool) Name() string                { return "cron_toggle" }
func (t *cronToggleTool) Capability() tool.Capability { return tool.CapWrite }
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
