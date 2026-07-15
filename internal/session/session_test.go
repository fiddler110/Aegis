package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/trace"
)

// fakeCheckpointCleaner records DeleteForSession calls without touching a
// real checkpoint.Store, for exercising the P32.3 Delete/Prune wiring.
type fakeCheckpointCleaner struct {
	deleted []string
}

func (f *fakeCheckpointCleaner) DeleteForSession(_ context.Context, sessionID string) error {
	f.deleted = append(f.deleted, sessionID)
	return nil
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestLegacyBlobMigration verifies that a session written the pre-P8.1 way
// (whole messages/traces JSON blobs on the sessions row, bypassing
// AppendMessages/AppendTraces) is transparently migrated into the row tables
// the next time the store is opened, and reads back identically.
func TestLegacyBlobMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sess, err := st.Create(context.Background(), "legacy", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: "hello"}}},
	}
	msgBlob, err := provider.MarshalMessages(msgs)
	if err != nil {
		t.Fatalf("MarshalMessages: %v", err)
	}
	traces := []trace.TurnTrace{{Index: 0, Model: "m", InputTokens: 10}}
	traceBlob, err := json.Marshal(traces)
	if err != nil {
		t.Fatalf("marshal traces: %v", err)
	}
	// Write directly to the legacy blob columns, simulating a database
	// created before P8.1 introduced row-per-message storage.
	if _, err := st.db.Exec(`UPDATE sessions SET messages = ?, traces = ? WHERE id = ?`, msgBlob, traceBlob, sess.ID); err != nil {
		t.Fatalf("seed legacy blobs: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening runs migrate() again, which should backfill the row tables
	// from the legacy blobs and reset them to '[]'.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	got, err := st2.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get after migration: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(got.Messages))
	}
	if len(got.Traces) != 1 || got.Traces[0].InputTokens != 10 {
		t.Fatalf("traces not migrated: %+v", got.Traces)
	}

	var legacyMsgBlob, legacyTraceBlob string
	if err := st2.db.QueryRow(`SELECT messages, traces FROM sessions WHERE id = ?`, sess.ID).Scan(&legacyMsgBlob, &legacyTraceBlob); err != nil {
		t.Fatalf("read legacy columns: %v", err)
	}
	if legacyMsgBlob != "[]" || legacyTraceBlob != "[]" {
		t.Errorf("legacy blob columns not reset after migration: messages=%q traces=%q", legacyMsgBlob, legacyTraceBlob)
	}

	// A subsequent append must continue from seq 2, not collide with the
	// migrated rows.
	if err := st2.AppendMessages(context.Background(), sess.ID, []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "more"}}},
	}); err != nil {
		t.Fatalf("AppendMessages after migration: %v", err)
	}
	got, err = st2.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get after append: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("got %d messages after append, want 3", len(got.Messages))
	}
}

// TestOpenAppliesPermissionHardening exercises the FIND-29/P24.16
// hardenDBPermissions call Open makes on the database file and its
// WAL-mode sidecars (-wal, -shm). On POSIX this is a no-op (fsguard's
// RestrictToOwner returns nil immediately there), so the main assertion is
// simply that Open still succeeds and the store is usable — a regression
// where the hardening call errors out (including on a sidecar that hasn't
// been created yet) must not break every session open.
func TestOpenAppliesPermissionHardening(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardened.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, err := st.Create(context.Background(), "hardening-check", "sys", "build", "", ""); err != nil {
		t.Fatalf("Create after Open: %v", err)
	}

	// Re-open the same path: hardenDBPermissions runs again and must not
	// error even though the -wal/-shm sidecars now exist from the first
	// open's writes above.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen after hardening: %v", err)
	}
	st2.Close()
}

func TestSessionRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "first", "be helpful", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.TextBlock{Text: "calling tool"},
			provider.ToolUseBlock{ID: "tu1", Name: "grep", Input: json.RawMessage(`{"pattern":"x"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "tu1", Content: "found", IsError: false},
		}},
	}
	if err := st.SaveMessages(ctx, sess.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "first" || got.System != "be helpful" || got.Mode != "build" {
		t.Errorf("metadata mismatch: %+v", got)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(got.Messages))
	}
	// Verify the tool-use block survived with its input intact.
	tu, ok := got.Messages[1].Content[1].(provider.ToolUseBlock)
	if !ok {
		t.Fatalf("expected ToolUseBlock, got %T", got.Messages[1].Content[1])
	}
	if tu.Name != "grep" || string(tu.Input) != `{"pattern":"x"}` {
		t.Errorf("tool-use block corrupted: %+v", tu)
	}
	tr, ok := got.Messages[2].Content[0].(provider.ToolResultBlock)
	if !ok || tr.ToolUseID != "tu1" || tr.Content != "found" {
		t.Errorf("tool-result block corrupted: %+v", got.Messages[2].Content[0])
	}
}

// TestCreatePersistsWorkdir verifies a session's own working directory
// (P25.1) round-trips through Create, Get, and List, and that leaving it
// empty (the pre-P25.1 default) still round-trips as empty rather than some
// other default.
func TestCreatePersistsWorkdir(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	withWorkdir, err := st.Create(ctx, "with-workdir", "sys", "build", "", "/tmp/some-project")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	noWorkdir, err := st.Create(ctx, "no-workdir", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := st.Get(ctx, withWorkdir.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Workdir != "/tmp/some-project" {
		t.Errorf("Workdir = %q, want /tmp/some-project", got.Workdir)
	}

	got2, err := st.Get(ctx, noWorkdir.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got2.Workdir != "" {
		t.Errorf("Workdir = %q, want empty", got2.Workdir)
	}

	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := make(map[string]string)
	for _, m := range list {
		found[m.ID] = m.Workdir
	}
	if found[withWorkdir.ID] != "/tmp/some-project" {
		t.Errorf("List: Workdir for %s = %q, want /tmp/some-project", withWorkdir.ID, found[withWorkdir.ID])
	}
	if found[noWorkdir.ID] != "" {
		t.Errorf("List: Workdir for %s = %q, want empty", noWorkdir.ID, found[noWorkdir.ID])
	}
}

func TestCreatePersistsPersona(t *testing.T) {
	store := newTestStore(t) // existing helper in this test file
	s, err := store.Create(context.Background(), "t", "sys", "build", "security-architect", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persona != "security-architect" {
		t.Errorf("persona = %q, want security-architect", got.Persona)
	}
}

func TestListAndDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a, _ := st.Create(ctx, "a", "", "plan", "", "")
	_, _ = st.Create(ctx, "b", "", "plan", "", "")

	metas, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("List returned %d, want 2", len(metas))
	}

	if err := st.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, a.ID); err != ErrNotFound {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

func TestGetMissing(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Get(context.Background(), "nope"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAppendTraces(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "traced", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Empty append is a no-op and must not error.
	if err := st.AppendTraces(ctx, sess.ID, nil); err != nil {
		t.Fatalf("AppendTraces(nil): %v", err)
	}

	// First run: two turns.
	run1 := []trace.TurnTrace{
		{Index: 0, Model: "claude-opus-4-8", InputTokens: 100, OutputTokens: 20, CostUSD: 0.003, WallMS: 1200,
			ToolCalls: []trace.ToolCall{{Name: "grep", DurationMS: 40}}},
		{Index: 1, Model: "claude-opus-4-8", InputTokens: 130, OutputTokens: 8, CostUSD: 0.002, WallMS: 800},
	}
	if err := st.AppendTraces(ctx, sess.ID, run1); err != nil {
		t.Fatalf("AppendTraces(run1): %v", err)
	}
	// Second run on the same session appends rather than overwrites.
	run2 := []trace.TurnTrace{
		{Index: 0, Model: "claude-opus-4-8", InputTokens: 50, OutputTokens: 5, CostUSD: 0.001, WallMS: 300},
	}
	if err := st.AppendTraces(ctx, sess.ID, run2); err != nil {
		t.Fatalf("AppendTraces(run2): %v", err)
	}

	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Traces) != 3 {
		t.Fatalf("got %d traces, want 3", len(got.Traces))
	}
	if got.Traces[0].ToolCalls[0].Name != "grep" || got.Traces[0].ToolCalls[0].DurationMS != 40 {
		t.Errorf("tool call not round-tripped: %+v", got.Traces[0].ToolCalls)
	}
	if got.Traces[2].InputTokens != 50 {
		t.Errorf("trace[2].InputTokens = %d, want 50", got.Traces[2].InputTokens)
	}
}

func TestAppendTracesMissingSession(t *testing.T) {
	st := newTestStore(t)
	err := st.AppendTraces(context.Background(), "nope", []trace.TurnTrace{{Index: 0}})
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestAppendMessagesIsIncremental verifies AppendMessages (the P8.1 hot path)
// adds only the new tail rather than rewriting the whole transcript, and that
// repeated calls accumulate rather than overwrite.
func TestAppendMessagesIsIncremental(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "incremental", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "one"}}},
	}
	if err := st.AppendMessages(ctx, sess.ID, first); err != nil {
		t.Fatalf("AppendMessages(first): %v", err)
	}
	second := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: "two"}}},
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "three"}}},
	}
	if err := st.AppendMessages(ctx, sess.ID, second); err != nil {
		t.Fatalf("AppendMessages(second): %v", err)
	}

	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(got.Messages))
	}
	wantTexts := []string{"one", "two", "three"}
	for i, want := range wantTexts {
		tb, ok := got.Messages[i].Content[0].(provider.TextBlock)
		if !ok || tb.Text != want {
			t.Errorf("message[%d] = %+v, want text %q", i, got.Messages[i], want)
		}
	}
}

func TestAppendMessagesMissingSession(t *testing.T) {
	st := newTestStore(t)
	err := st.AppendMessages(context.Background(), "nope", []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	})
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestSaveMessagesTruncates verifies SaveMessages (used by rewind) fully
// replaces the transcript rather than appending.
func TestSaveMessagesTruncates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "truncate", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	full := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "a"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: "b"}}},
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "c"}}},
	}
	if err := st.AppendMessages(ctx, sess.ID, full); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}
	if err := st.SaveMessages(ctx, sess.ID, full[:1]); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}
	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(got.Messages))
	}
	// A subsequent append should resume from the truncated point, not the
	// original (pre-truncation) sequence.
	if err := st.AppendMessages(ctx, sess.ID, []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: "d"}}},
	}); err != nil {
		t.Fatalf("AppendMessages after truncate: %v", err)
	}
	got, err = st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(got.Messages))
	}
	tb, ok := got.Messages[1].Content[0].(provider.TextBlock)
	if !ok || tb.Text != "d" {
		t.Errorf("message[1] = %+v, want text 'd'", got.Messages[1])
	}
}

// TestDeleteRemovesMessageAndTraceRows ensures Delete cleans up the P8.1 row
// tables rather than leaking them once the parent session is gone.
func TestDeleteRemovesMessageAndTraceRows(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "del", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.AppendMessages(ctx, sess.ID, []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}
	if err := st.AppendTraces(ctx, sess.ID, []trace.TurnTrace{{Index: 0}}); err != nil {
		t.Fatalf("AppendTraces: %v", err)
	}
	if err := st.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var msgCount, traceCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sess.ID).Scan(&msgCount); err != nil {
		t.Fatalf("count session_messages: %v", err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM session_traces WHERE session_id = ?`, sess.ID).Scan(&traceCount); err != nil {
		t.Fatalf("count session_traces: %v", err)
	}
	if msgCount != 0 || traceCount != 0 {
		t.Errorf("Delete left orphan rows: messages=%d traces=%d", msgCount, traceCount)
	}
}

// TestDeleteRemovesBGEventsAndCheckpoints covers P32.3: Delete must clean up
// bg_events rows and fan out to a registered CheckpointCleaner, not just the
// session_messages/session_traces rows TestDeleteRemovesMessageAndTraceRows
// already covered — previously only the HTTP delete-session handler did the
// checkpoint half of this, and nothing cleaned up bg_events at all.
func TestDeleteRemovesBGEventsAndCheckpoints(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	cleaner := &fakeCheckpointCleaner{}
	st.SetCheckpointCleaner(cleaner)

	sess, err := st.Create(ctx, "del-bg", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.AppendBGEvent(ctx, sess.ID, `{"kind":"text"}`); err != nil {
		t.Fatalf("AppendBGEvent: %v", err)
	}

	if err := st.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var bgCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM bg_events WHERE session_id = ?`, sess.ID).Scan(&bgCount); err != nil {
		t.Fatalf("count bg_events: %v", err)
	}
	if bgCount != 0 {
		t.Errorf("Delete left %d orphan bg_events rows", bgCount)
	}
	if len(cleaner.deleted) != 1 || cleaner.deleted[0] != sess.ID {
		t.Errorf("expected checkpoint cleaner called once with %q, got %v", sess.ID, cleaner.deleted)
	}
}

// TestPruneRemovesBGEventsAndCheckpoints covers P32.3's other leak path: the
// TTL auto-pruner and /sessions/prune both call Store.Prune directly, which
// previously bypassed checkpoint (and bg_events) cleanup entirely since only
// the HTTP delete-session handler called checkpoints.DeleteForSession.
func TestPruneRemovesBGEventsAndCheckpoints(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	cleaner := &fakeCheckpointCleaner{}
	st.SetCheckpointCleaner(cleaner)

	old, err := st.Create(ctx, "old", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.AppendBGEvent(ctx, old.ID, `{"kind":"text"}`); err != nil {
		t.Fatalf("AppendBGEvent: %v", err)
	}
	// Backdate updated_at so Prune's TTL threshold catches it.
	backdated := time.Now().Add(-48 * time.Hour).UnixMilli()
	if _, err := st.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, backdated, old.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	fresh, err := st.Create(ctx, "fresh", "sys", "build", "", "")
	if err != nil {
		t.Fatalf("Create fresh: %v", err)
	}

	n, err := st.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("Prune deleted %d sessions, want 1", n)
	}

	var bgCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM bg_events WHERE session_id = ?`, old.ID).Scan(&bgCount); err != nil {
		t.Fatalf("count bg_events: %v", err)
	}
	if bgCount != 0 {
		t.Errorf("Prune left %d orphan bg_events rows for pruned session", bgCount)
	}
	if len(cleaner.deleted) != 1 || cleaner.deleted[0] != old.ID {
		t.Errorf("expected checkpoint cleaner called once with %q, got %v", old.ID, cleaner.deleted)
	}
	if _, err := st.Get(ctx, fresh.ID); err != nil {
		t.Errorf("fresh session should survive Prune: %v", err)
	}
}

// TestTodayCostDefaultsToZero verifies a fresh store with no recorded spend
// reports zero rather than erroring (P9.5 daily spend cap).
func TestTodayCostDefaultsToZero(t *testing.T) {
	st := newTestStore(t)
	got, err := st.TodayCost(context.Background())
	if err != nil {
		t.Fatalf("TodayCost: %v", err)
	}
	if got != 0 {
		t.Errorf("TodayCost on empty store = %v, want 0", got)
	}
}

// TestAddDailyCostAccumulates verifies repeated calls sum into the same day's
// row instead of overwriting it (P9.5 daily spend cap).
func TestAddDailyCostAccumulates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.AddDailyCost(ctx, 1.5); err != nil {
		t.Fatalf("AddDailyCost: %v", err)
	}
	if err := st.AddDailyCost(ctx, 2.25); err != nil {
		t.Fatalf("AddDailyCost: %v", err)
	}
	got, err := st.TodayCost(ctx)
	if err != nil {
		t.Fatalf("TodayCost: %v", err)
	}
	const want = 3.75
	if got != want {
		t.Errorf("TodayCost = %v, want %v", got, want)
	}
}

// TestTodayTokensDefaultsToZero is the P10.5 token-cap counterpart to
// TestTodayCostDefaultsToZero.
func TestTodayTokensDefaultsToZero(t *testing.T) {
	st := newTestStore(t)
	got, err := st.TodayTokens(context.Background())
	if err != nil {
		t.Fatalf("TodayTokens: %v", err)
	}
	if got != 0 {
		t.Errorf("TodayTokens on empty store = %v, want 0", got)
	}
}

// TestAddDailyTokensAccumulates verifies repeated calls sum into the same
// day's row instead of overwriting it (P10.5 daily token cap).
func TestAddDailyTokensAccumulates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.AddDailyTokens(ctx, 150); err != nil {
		t.Fatalf("AddDailyTokens: %v", err)
	}
	if err := st.AddDailyTokens(ctx, 225); err != nil {
		t.Fatalf("AddDailyTokens: %v", err)
	}
	got, err := st.TodayTokens(ctx)
	if err != nil {
		t.Fatalf("TodayTokens: %v", err)
	}
	const want = 375
	if got != want {
		t.Errorf("TodayTokens = %v, want %v", got, want)
	}
}
