package swarm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestMailboxSendReadMarkRead(t *testing.T) {
	root := t.TempDir()
	id := NewIdentity("worker", "team-a", "sess-1")
	mb, err := OpenMailbox(root, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := mb.Send(Message{Type: MsgResult, Sender: id.AgentID, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := mb.Send(Message{Type: MsgUser, Sender: "parent", Text: "second"}); err != nil {
		t.Fatal(err)
	}

	msgs, err := mb.ReadAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Text != "first" || msgs[1].Text != "second" {
		t.Errorf("messages out of order: %q, %q", msgs[0].Text, msgs[1].Text)
	}
	if msgs[0].ID == "" || msgs[0].Timestamp.IsZero() {
		t.Error("Send should fill ID and Timestamp")
	}

	// Marking one read excludes it from the unread view.
	if err := mb.MarkRead(msgs[0].ID); err != nil {
		t.Fatal(err)
	}
	unread, _ := mb.ReadAll(true)
	if len(unread) != 1 || unread[0].Text != "second" {
		t.Errorf("after MarkRead, unread = %+v", unread)
	}
	// But all are still visible without the unread filter.
	if all, _ := mb.ReadAll(false); len(all) != 2 {
		t.Errorf("ReadAll(false) = %d, want 2", len(all))
	}
}

// TestMarkReadEvictsFromInbox verifies MarkRead physically moves the message
// out of the inbox directory (P8.3) rather than leaving it there forever with
// just its Read flag flipped — otherwise every unread-only poll of a
// long-running mailbox keeps re-scanning and re-parsing it.
func TestMarkReadEvictsFromInbox(t *testing.T) {
	root := t.TempDir()
	id := NewIdentity("worker", "team-a", "sess-1")
	mb, err := OpenMailbox(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.Send(Message{Type: MsgResult, Sender: id.AgentID, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := mb.ReadAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.MarkRead(msgs[0].ID); err != nil {
		t.Fatal(err)
	}

	dir := inboxDir(root, id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			t.Errorf("read message file %q left in inbox dir, want moved to processed/", e.Name())
		}
	}
	processed, err := os.ReadDir(filepath.Join(dir, processedSubdir))
	if err != nil {
		t.Fatalf("processed dir: %v", err)
	}
	if len(processed) != 1 {
		t.Errorf("processed dir has %d entries, want 1", len(processed))
	}

	// A second MarkRead on the same (now-moved) id must stay a no-op.
	if err := mb.MarkRead(msgs[0].ID); err != nil {
		t.Fatalf("MarkRead on already-processed id: %v", err)
	}
}

// TestMarkReadEvictsOldProcessedFiles is the P9 regression: MarkRead moves
// read messages into processed/ instead of deleting them (P8.3), but nothing
// previously evicted them — a long-running or chatty team accumulated files
// there indefinitely. MarkRead should now sweep processed/ for entries older
// than processedRetention on every call.
func TestMarkReadEvictsOldProcessedFiles(t *testing.T) {
	root := t.TempDir()
	id := NewIdentity("worker", "team-a", "sess-1")
	mb, err := OpenMailbox(root, id)
	if err != nil {
		t.Fatal(err)
	}

	// Plant a processed file older than the retention window, as if it had
	// been sitting there since a much earlier MarkRead call.
	if err := os.MkdirAll(mb.processedDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(mb.processedDir(), "old.json")
	if err := os.WriteFile(oldPath, []byte(`{"id":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-processedRetention - time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// A fresh message, marked read, lands in processed/ too and should
	// trigger an eviction sweep as a side effect.
	if err := mb.Send(Message{Type: MsgResult, Sender: id.AgentID, Text: "fresh"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := mb.ReadAll(true)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("ReadAll: %v (n=%d)", err, len(msgs))
	}
	if err := mb.MarkRead(msgs[0].ID); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old processed file should have been evicted, stat err = %v", err)
	}
	processed, err := os.ReadDir(mb.processedDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(processed) != 1 {
		t.Errorf("processed dir has %d entries after eviction, want 1 (only the fresh, non-evictable message)", len(processed))
	}
}

func TestMailboxSkipsCorruptAndTemp(t *testing.T) {
	root := t.TempDir()
	id := NewIdentity("w", "t", "")
	mb, err := OpenMailbox(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.Send(Message{Text: "good"}); err != nil {
		t.Fatal(err)
	}
	dir := inboxDir(root, id)
	// A half-written temp file and a corrupt json file must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "999_partial.json.tmp"), []byte("{partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "998_bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := mb.ReadAll(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "good" {
		t.Errorf("expected only the good message, got %+v", msgs)
	}
}

func TestMailboxMissingDirIsEmpty(t *testing.T) {
	mb := &Mailbox{dir: filepath.Join(t.TempDir(), "does-not-exist")}
	msgs, err := mb.ReadAll(false)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected no messages, got %d", len(msgs))
	}
}

// TestMarkReadWritesProcessedFileOwnerOnly is the FIND-20/P27.11 regression:
// the processed/ copy MarkRead writes used to be world-readable (0o644),
// unlike every other message file in the mailbox (0o600 via Send, 0o700 via
// the MkdirAll calls). On POSIX the mode bit itself is authoritative, so
// assert it directly; on Windows the mode bit is not meaningful (see
// fsguard's package doc), so this just confirms MarkRead still succeeds —
// the ACL side of the hardening is covered by
// TestOpenMailboxHardensRootDirectory below.
func TestMarkReadWritesProcessedFileOwnerOnly(t *testing.T) {
	root := t.TempDir()
	id := NewIdentity("worker", "team-a", "sess-1")
	mb, err := OpenMailbox(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.Send(Message{Type: MsgResult, Sender: id.AgentID, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := mb.ReadAll(true)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("ReadAll: %v (n=%d)", err, len(msgs))
	}
	if err := mb.MarkRead(msgs[0].ID); err != nil {
		t.Fatal(err)
	}

	processed, err := os.ReadDir(mb.processedDir())
	if err != nil {
		t.Fatalf("processed dir: %v", err)
	}
	if len(processed) != 1 {
		t.Fatalf("processed dir has %d entries, want 1", len(processed))
	}
	info, err := processed[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("processed file mode = %o, want 0600 (was 0644 before P27.11)", perm)
		}
	}
}

// TestOpenMailboxHardensRootDirectory is the directory-ACL half of
// FIND-20/P27.11: OpenMailbox must apply fsguard.RestrictToOwner to the
// mailbox root (the `teams/` tree), not just leave it to the MkdirAll mode
// bit that Windows ignores. This is a smoke test in the same spirit as
// session.TestOpenAppliesPermissionHardening and
// config.TestLoadDotEnvAppliesPermissionHardening: the ACL mechanics
// themselves are covered by internal/fsguard's own tests, so this only
// confirms the call is wired in, doesn't error, and doesn't block repeated
// opens of an already-existing root (e.g. one teammate opening its mailbox
// after another already created the tree).
func TestOpenMailboxHardensRootDirectory(t *testing.T) {
	dataDir := t.TempDir()
	root := MailboxRoot(dataDir)

	id1 := NewIdentity("worker-1", "team-a", "sess-1")
	if _, err := OpenMailbox(root, id1); err != nil {
		t.Fatalf("OpenMailbox (first, creates root): %v", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("mailbox root %s not created: %v", root, err)
	}

	// A second teammate opening its own mailbox re-hardens the
	// already-existing root; this must not error or make the root
	// inaccessible to the process that owns it.
	id2 := NewIdentity("worker-2", "team-a", "sess-1")
	mb2, err := OpenMailbox(root, id2)
	if err != nil {
		t.Fatalf("OpenMailbox (second, root pre-exists): %v", err)
	}
	if err := mb2.Send(Message{Type: MsgResult, Sender: id2.AgentID, Text: "hi"}); err != nil {
		t.Fatalf("Send after root hardening: %v", err)
	}
}
