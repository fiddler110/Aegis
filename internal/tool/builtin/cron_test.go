package builtin

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/cron"
	"github.com/scottymacleod/agentharness/internal/tool"

	_ "modernc.org/sqlite"
)

func cronTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func cronTestScheduler(t *testing.T) *cron.Scheduler {
	t.Helper()
	db := cronTestDB(t)
	store, err := cron.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return cron.NewScheduler(store, func(j cron.Job) {}, nil)
}

func cronToolByName(tools []tool.Tool, name string) tool.Tool {
	for _, tl := range tools {
		if tl.Name() == name {
			return tl
		}
	}
	return nil
}

func TestCronCreateAndList(t *testing.T) {
	sched := cronTestScheduler(t)
	tools := CronTools(sched)
	ctx := context.Background()

	create := cronToolByName(tools, "cron_create")
	list := cronToolByName(tools, "cron_list")

	res, err := create.Execute(ctx, json.RawMessage(`{"schedule":"@hourly","command":"echo hello","title":"hourly echo"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "Created cron job") {
		t.Errorf("unexpected result: %s", res.Content)
	}

	res, err = list.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "(no cron jobs)") {
		t.Error("expected at least one job in list")
	}
	if !strings.Contains(res.Content, "echo hello") {
		t.Errorf("expected command in list output: %s", res.Content)
	}
}

func TestCronCreateBadSchedule(t *testing.T) {
	sched := cronTestScheduler(t)
	tools := CronTools(sched)
	ctx := context.Background()

	create := cronToolByName(tools, "cron_create")
	res, err := create.Execute(ctx, json.RawMessage(`{"schedule":"bad","command":"echo"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error for bad schedule")
	}
}

func TestCronToggleAndDelete(t *testing.T) {
	sched := cronTestScheduler(t)
	tools := CronTools(sched)
	ctx := context.Background()

	create := cronToolByName(tools, "cron_create")
	toggle := cronToolByName(tools, "cron_toggle")
	del := cronToolByName(tools, "cron_delete")
	list := cronToolByName(tools, "cron_list")

	res, _ := create.Execute(ctx, json.RawMessage(`{"schedule":"@daily","command":"echo daily"}`))
	id := extractCronID(res.Content)
	if id == "" {
		t.Fatalf("could not extract id from: %s", res.Content)
	}

	// Toggle off
	res, err := toggle.Execute(ctx, json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "disabled") {
		t.Errorf("expected disabled, got: %s", res.Content)
	}

	// Toggle on
	res, _ = toggle.Execute(ctx, json.RawMessage(`{"id":"`+id+`"}`))
	if !strings.Contains(res.Content, "enabled") {
		t.Errorf("expected enabled, got: %s", res.Content)
	}

	// Delete
	res, err = del.Execute(ctx, json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "deleted") {
		t.Errorf("expected deleted, got: %s", res.Content)
	}

	// List should be empty
	res, _ = list.Execute(ctx, nil)
	if !strings.Contains(res.Content, "(no cron jobs)") {
		t.Errorf("expected empty list, got: %s", res.Content)
	}
}

func TestCronDeleteNotFound(t *testing.T) {
	sched := cronTestScheduler(t)
	tools := CronTools(sched)
	ctx := context.Background()

	del := cronToolByName(tools, "cron_delete")
	res, err := del.Execute(ctx, json.RawMessage(`{"id":"nonexistent"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error for nonexistent id")
	}
}

func TestCronCreateMissingFields(t *testing.T) {
	sched := cronTestScheduler(t)
	tools := CronTools(sched)
	ctx := context.Background()

	create := cronToolByName(tools, "cron_create")

	res, _ := create.Execute(ctx, json.RawMessage(`{"schedule":"@hourly"}`))
	if !res.IsError {
		t.Error("expected error for missing command")
	}

	res, _ = create.Execute(ctx, json.RawMessage(`{"command":"echo hi"}`))
	if !res.IsError {
		t.Error("expected error for missing schedule")
	}
}

func extractCronID(s string) string {
	const marker = "(id "
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	s = s[i+len(marker):]
	j := strings.Index(s, ")")
	if j < 0 {
		return ""
	}
	return s[:j]
}
