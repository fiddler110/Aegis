package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// MessageType classifies a mailbox message.
type MessageType string

const (
	MsgResult     MessageType = "result"       // a teammate's final output
	MsgUser       MessageType = "user_message" // a steering message to a teammate
	MsgShutdown   MessageType = "shutdown"     // ask a teammate to stop
	MsgIdleNotify MessageType = "idle"         // a teammate reports it is idle
	MsgPeer       MessageType = "peer"         // a direct teammate-to-teammate message (P5.1)
)

// SendTo delivers a message to another teammate's inbox under root, opening
// (creating) the recipient's mailbox as needed. This enables peer-to-peer
// messaging, not just parent→child delivery.
func SendTo(root string, recipient Identity, msg Message) error {
	mb, err := OpenMailbox(root, recipient)
	if err != nil {
		return err
	}
	msg.Recipient = recipient.AgentID
	return mb.Send(msg)
}

// Message is one item in a mailbox. Payload carries type-specific data.
type Message struct {
	ID        string         `json:"id"`
	Type      MessageType    `json:"type"`
	Sender    string         `json:"sender"`
	Recipient string         `json:"recipient"`
	Text      string         `json:"text,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Read      bool           `json:"read"`
}

// Mailbox is a durable, file-backed message queue for one teammate. Messages
// are individual JSON files written atomically (temp file + rename) so readers
// never observe a partial message.
type Mailbox struct {
	dir string // the inbox directory
}

// MailboxRoot returns the on-disk root for all team mailboxes under dataDir.
func MailboxRoot(dataDir string) string {
	return filepath.Join(dataDir, "teams")
}

// inboxDir returns the inbox path for an agent under root (root is MailboxRoot).
func inboxDir(root string, id Identity) string {
	return filepath.Join(root, sanitize(id.Team), "agents", sanitize(id.AgentID), "inbox")
}

// OpenMailbox opens (creating if needed) the inbox for id under root.
func OpenMailbox(root string, id Identity) (*Mailbox, error) {
	dir := inboxDir(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("swarm: create mailbox %s: %w", dir, err)
	}
	hardenMailboxRoot(root)
	return &Mailbox{dir: dir}, nil
}

// hardenMailboxRoot applies fsguard.RestrictToOwner (FIND-20/P27.11) to the
// mailbox root directory (the `teams/` tree), matching the pattern other
// on-disk stores in this codebase use to lock their storage location down on
// Windows — see session.hardenDBPermissions and workspacetrust.Store.save.
// The mode bits passed to MkdirAll above already restrict every directory to
// its owner on POSIX; Windows ignores MkdirAll's mode argument entirely and a
// directory under a shared data dir otherwise inherits its parent's
// (potentially broader) ACL. Best-effort and called on every OpenMailbox, not
// just first creation, so an already-existing root created before this
// hardening landed still gets locked down; a failure here is only logged
// since it shouldn't block the mailbox open that already succeeded.
func hardenMailboxRoot(root string) {
	if err := fsguard.RestrictToOwner(root); err != nil {
		slog.Default().Warn("failed to restrict swarm mailbox root permissions", "path", root, "err", err)
	}
}

// Send appends a message atomically. ID and Timestamp are filled if unset.
func (m *Mailbox) Send(msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("swarm: marshal message: %w", err)
	}
	// Filename sorts chronologically: <unixnano>_<id>.json.
	name := fmt.Sprintf("%020d_%s.json", msg.Timestamp.UnixNano(), msg.ID)
	final := filepath.Join(m.dir, name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("swarm: write message: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("swarm: commit message: %w", err)
	}
	return nil
}

// processedSubdir holds messages already marked read, out of ReadAll's
// unread-only scan path (P8.3 — otherwise every already-read message is
// re-listed, re-read, and re-parsed on every poll of a long-running mailbox).
const processedSubdir = "processed"

func (m *Mailbox) processedDir() string { return filepath.Join(m.dir, processedSubdir) }

// readMessagesFrom lists and decodes every message file directly in dir
// (non-recursive). Corrupt or partial files are skipped rather than failing
// the whole read.
func readMessagesFrom(dir string) ([]Message, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("swarm: read mailbox: %w", err)
	}
	var out []Message
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue // skip subdirs (e.g. processed/), .tmp, and stray files
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // skip corrupt/partial
		}
		out = append(out, msg)
	}
	return out, nil
}

// ReadAll returns messages sorted by timestamp. When unreadOnly is true, only
// the inbox's unread messages are scanned — already-read messages live under
// processed/ (moved there by MarkRead) and are skipped entirely rather than
// read and filtered out (P8.3). When unreadOnly is false, processed messages
// are included too, for callers that want full history.
func (m *Mailbox) ReadAll(unreadOnly bool) ([]Message, error) {
	out, err := readMessagesFrom(m.dir)
	if err != nil {
		return nil, err
	}
	if !unreadOnly {
		processed, err := readMessagesFrom(m.processedDir())
		if err != nil {
			return nil, err
		}
		out = append(out, processed...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, nil
}

// processedRetention bounds how long a message stays under processed/ before
// evictProcessed removes it (P9). MarkRead moves read messages here rather
// than deleting them outright (P8.3, so an unread-only scan never has to look
// at them again), but nothing previously evicted them — a long-running or
// chatty team accumulated files under processed/ indefinitely.
const processedRetention = 7 * 24 * time.Hour

// MarkRead flips the read flag for the message with the given id and moves it
// out of the inbox into processed/, so future unread-only scans never have to
// look at it again (P8.3). It is a no-op if the message is absent. Before
// returning, it opportunistically evicts processed/ entries older than
// processedRetention — no separate background sweep is needed since MarkRead
// is the only path that adds to processed/ in the first place.
func (m *Mailbox) MarkRead(id string) error {
	defer m.evictProcessed()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || !strings.Contains(e.Name(), id) {
			continue
		}
		path := filepath.Join(m.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		msg.Read = true
		out, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(m.processedDir(), 0o700); err != nil {
			return err
		}
		dest := filepath.Join(m.processedDir(), e.Name())
		tmp := dest + ".tmp"
		if err := os.WriteFile(tmp, out, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return os.Remove(path)
	}
	return nil
}

// evictProcessed removes processed/ message files older than
// processedRetention. Best-effort: a listing failure or an individual file
// that can't be removed is silently skipped rather than failing the calling
// MarkRead, since eviction is housekeeping, not correctness-critical.
func (m *Mailbox) evictProcessed() {
	dir := m.processedDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-processedRetention)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// sanitize makes a string safe for use as a single path segment.
func sanitize(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}
	return strings.Map(repl, s)
}
