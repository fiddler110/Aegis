package swarm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MessageType classifies a mailbox message.
type MessageType string

const (
	MsgResult     MessageType = "result"       // a teammate's final output
	MsgUser       MessageType = "user_message" // a steering message to a teammate
	MsgShutdown   MessageType = "shutdown"     // ask a teammate to stop
	MsgIdleNotify MessageType = "idle"         // a teammate reports it is idle
)

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
	return &Mailbox{dir: dir}, nil
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

// ReadAll returns messages sorted by timestamp. Corrupt or partial files are
// skipped rather than failing the whole read. When unreadOnly is true, messages
// already marked read are excluded.
func (m *Mailbox) ReadAll(unreadOnly bool) ([]Message, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("swarm: read mailbox: %w", err)
	}
	var out []Message
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue // skip .tmp and stray files
		}
		data, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // skip corrupt/partial
		}
		if unreadOnly && msg.Read {
			continue
		}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, nil
}

// MarkRead flips the read flag for the message with the given id, rewriting it
// atomically. It is a no-op if the message is absent.
func (m *Mailbox) MarkRead(id string) error {
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
		if msg.Read {
			return nil
		}
		msg.Read = true
		out, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, out, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	return nil
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
