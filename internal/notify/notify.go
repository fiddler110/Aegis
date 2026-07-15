// Package notify delivers notifications when a background session finishes,
// errors, or needs input (P5.4). It supports OS desktop notifications and an
// optional webhook that receives the event as JSON.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// Status classifies a notification.
type Status string

const (
	StatusCompleted  Status = "completed"
	StatusError      Status = "error"
	StatusNeedsInput Status = "needs_input"
)

// Event is one notification payload.
type Event struct {
	SessionID string  `json:"session_id"`
	Title     string  `json:"title,omitempty"`
	Status    Status  `json:"status"`
	Message   string  `json:"message,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
	Time      string  `json:"time"`
}

// Notifier delivers notifications via the configured channels.
type Notifier struct {
	desktop bool
	webhook string
	client  *http.Client
	logger  *slog.Logger
}

// New builds a Notifier. Returns nil when no channel is enabled so callers can
// skip wiring entirely.
func New(desktop bool, webhook string, logger *slog.Logger) *Notifier {
	if !desktop && webhook == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		desktop: desktop,
		webhook: webhook,
		client:  &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}
}

// Notify delivers an event over all configured channels. It never blocks the
// caller for long and swallows delivery errors (logging them).
func (n *Notifier) Notify(ctx context.Context, ev Event) {
	if n == nil {
		return
	}
	if ev.Time == "" {
		ev.Time = time.Now().Format(time.RFC3339)
	}
	if n.desktop {
		n.sendDesktop(ev)
	}
	if n.webhook != "" {
		n.sendWebhook(ctx, ev)
	}
}

func (n *Notifier) sendWebhook(ctx context.Context, ev Event) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhook, bytes.NewReader(body))
	if err != nil {
		n.logger.Warn("notify webhook build failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.logger.Warn("notify webhook failed", "err", err)
		return
	}
	resp.Body.Close()
}

// desktopTitle is the notification title shown by the OS.
const desktopTitle = "Aegis"

func (n *Notifier) sendDesktop(ev Event) {
	msg := ev.Message
	if msg == "" {
		msg = "session " + ev.SessionID + " " + string(ev.Status)
	}
	cmd := desktopCommand(msg, ev.Title)
	if cmd == nil {
		return
	}
	if err := cmd.Run(); err != nil {
		n.logger.Warn("desktop notification failed", "err", err)
	}
}

// desktopCommand returns the platform-specific notification command, or nil on
// unsupported platforms / when the tool is missing.
func desktopCommand(message, subtitle string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + osaQuote(message) + " with title " + osaQuote(desktopTitle)
		if subtitle != "" {
			script += " subtitle " + osaQuote(subtitle)
		}
		return exec.Command("osascript", "-e", script)
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return nil
		}
		title := desktopTitle
		if subtitle != "" {
			title += " — " + subtitle
		}
		return exec.Command("notify-send", title, message)
	case "windows":
		ps := "New-BurntToastNotification -Text '" + desktopTitle + "','" + winEscape(message) + "'"
		return exec.Command(sandbox.WindowsShellBinary(), "-NoProfile", "-Command", ps)
	default:
		return nil
	}
}

// osaQuote wraps a string as an AppleScript string literal, escaping quotes and
// backslashes.
func osaQuote(s string) string {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// winEscape escapes single quotes for a PowerShell single-quoted string.
func winEscape(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	return string(out)
}
