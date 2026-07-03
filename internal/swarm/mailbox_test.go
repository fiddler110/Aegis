package swarm

import (
	"os"
	"path/filepath"
	"testing"
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
