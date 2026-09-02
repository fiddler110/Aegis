package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// TestUpdateCronJobsPopulatesSidebar guards the P<dashboard> live cron-job
// listing: a silent ListCronJobs poll result must land in m.cronJobs and
// show up in the sidebar's CRON section alongside the existing aggregate
// count, without requiring any change from an older daemon that never sends
// this message at all (m.cronJobs simply stays nil in that case).
func TestUpdateCronJobsPopulatesSidebar(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.sidebarOpen = true
	m.cronJobCount = 1
	m.layout()

	m = driveUpdate(t, m, cronJobsUpdateMsg{items: []api.CronJobInfo{
		{ID: "j1", Title: "nightly scan", Schedule: "0 2 * * *"},
	}})

	if len(m.cronJobs) != 1 || m.cronJobs[0].Title != "nightly scan" {
		t.Fatalf("expected m.cronJobs populated with the polled job, got %+v", m.cronJobs)
	}
	got := plainView(m)
	if !strings.Contains(got, "nightly scan") {
		t.Fatalf("expected the sidebar to list the cron job title, got:\n%s", got)
	}
}
