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
	"os"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/session"
)

// Client talks to a running harness daemon.
type Client struct {
	base      string
	http      *http.Client // no timeout — used for SSE streaming
	httpShort *http.Client // 30s timeout — used for non-streaming RPCs
	authToken string
}

// New returns a client for the daemon at addr (host:port).
func New(addr string) *Client {
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		base:      strings.TrimRight(base, "/"),
		http:      &http.Client{Timeout: 0},
		httpShort: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithToken returns a copy of c that authenticates with the given token.
// The returned copy shares the underlying HTTP transports.
func (c *Client) WithToken(token string) *Client {
	c2 := *c
	c2.authToken = token
	return &c2
}

// WithTokenFile reads the auth token from path and returns an authenticated
// copy of c. If the file cannot be read, c is returned unchanged.
func (c *Client) WithTokenFile(path string) *Client {
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	return c.WithToken(strings.TrimSpace(string(data)))
}

func (c *Client) setAuth(req *http.Request) {
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// Health checks daemon reachability.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	resp, err := c.httpShort.Do(req)
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

// Teammates returns the sub-agents tracked by the daemon's swarm registry.
func (c *Client) Teammates(ctx context.Context) ([]api.Teammate, error) {
	var out []api.Teammate
	if err := c.do(ctx, http.MethodGet, "/teammates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateSession patches a session's system prompt and/or mode.
func (c *Client) UpdateSession(ctx context.Context, id string, req api.UpdateSessionRequest) (*api.SessionMeta, error) {
	var out api.SessionMeta
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCommands returns custom slash commands from the daemon.
func (c *Client) ListCommands(ctx context.Context) ([]api.CommandInfo, error) {
	var out []api.CommandInfo
	if err := c.do(ctx, http.MethodGet, "/commands", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMemory returns the current memory and skills state.
func (c *Client) GetMemory(ctx context.Context) (*api.MemoryResponse, error) {
	var out api.MemoryResponse
	if err := c.do(ctx, http.MethodGet, "/memory", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendMemory adds a memory entry via the daemon.
func (c *Client) AppendMemory(ctx context.Context, req api.AppendMemoryRequest) error {
	return c.do(ctx, http.MethodPost, "/memory", req, nil)
}

// ListPersonas returns available persona names and descriptions.
func (c *Client) ListPersonas(ctx context.Context) ([]api.PersonaInfo, error) {
	var out []api.PersonaInfo
	if err := c.do(ctx, http.MethodGet, "/personas", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	c.setAuth(req)

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
	c.setAuth(req)
	resp, err := c.httpShort.Do(req)
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
