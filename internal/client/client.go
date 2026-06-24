// Package client is a thin HTTP/SSE client for the harness daemon.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scottymacleod/agentharness/internal/api"
	"github.com/scottymacleod/agentharness/internal/session"
)

// Client talks to a running harness daemon.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the daemon at addr (host:port).
func New(addr string) *Client {
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 0}}
}

// Health checks daemon reachability.
func (c *Client) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon unhealthy: %d", resp.StatusCode)
	}
	return nil
}

// CreateSession creates a new session.
func (c *Client) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.SessionMeta, error) {
	var out api.SessionMeta
	if err := c.do(ctx, http.MethodPost, "/sessions", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessions returns session metadata.
func (c *Client) ListSessions(ctx context.Context) ([]api.SessionMeta, error) {
	var out []api.SessionMeta
	if err := c.do(ctx, http.MethodGet, "/sessions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSession returns a full session including messages.
func (c *Client) GetSession(ctx context.Context, id string) (*session.Session, error) {
	var out session.Session
	if err := c.do(ctx, http.MethodGet, "/sessions/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession removes a session.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+id, nil, nil)
}

// PostMessage streams engine events for a user turn. Events are delivered on
// the returned channel, which is closed when the run finishes or ctx is done.
func (c *Client) PostMessage(ctx context.Context, id, text string) (<-chan api.Event, error) {
	body, _ := json.Marshal(api.PostMessageRequest{Text: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/sessions/"+id+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}

	out := make(chan api.Event)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			rest, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			var ev api.Event
			if err := json.Unmarshal([]byte(strings.TrimSpace(rest)), &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var bodyReader io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	var e api.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&e); err == nil && e.Error != "" {
		return fmt.Errorf("daemon error (%d): %s", resp.StatusCode, e.Error)
	}
	return fmt.Errorf("daemon error: %d", resp.StatusCode)
}
