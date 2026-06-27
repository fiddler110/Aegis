package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ServerConfig configures one LSP server.
type ServerConfig struct {
	Name       string   `koanf:"name"`       // e.g. "gopls", "pyright"
	Command    string   `koanf:"command"`    // executable to launch
	Args       []string `koanf:"args"`       // CLI arguments
	Extensions []string `koanf:"extensions"` // file extensions this server handles (e.g. [".go"])
}

// Manager owns LSP server lifecycles and routes requests to the right server
// based on file extension.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*Client // name → client
	extMap  map[string]*Client // ".go" → client
	rootURI string
	logger  *slog.Logger
}

// NewManager creates an LSP manager for the given workspace root.
func NewManager(rootDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		servers: make(map[string]*Client),
		extMap:  make(map[string]*Client),
		rootURI: fileURI(rootDir),
		logger:  logger,
	}
}

// Start launches an LSP server if not already running.
func (m *Manager) Start(ctx context.Context, cfg ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.servers[cfg.Name]; ok {
		return nil
	}

	client, err := NewClient(ctx, cfg.Name, cfg.Command, cfg.Args, m.rootURI)
	if err != nil {
		return fmt.Errorf("lsp: start %s: %w", cfg.Name, err)
	}

	m.servers[cfg.Name] = client
	for _, ext := range cfg.Extensions {
		ext = strings.ToLower(ext)
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		m.extMap[ext] = client
	}
	m.logger.Info("lsp server started", "name", cfg.Name, "extensions", cfg.Extensions)
	return nil
}

// ClientForFile returns the LSP client that handles the given file path, or
// nil if no server is configured for that extension.
func (m *Manager) ClientForFile(path string) *Client {
	ext := strings.ToLower(filepath.Ext(path))
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.extMap[ext]
}

// ClientByName returns a specific LSP client by server name.
func (m *Manager) ClientByName(name string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers[name]
}

// ServerNames returns the names of all started servers.
func (m *Manager) ServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.servers))
	for n := range m.servers {
		names = append(names, n)
	}
	return names
}

// Close shuts down all managed servers.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, c := range m.servers {
		if err := c.Close(); err != nil {
			m.logger.Warn("lsp server close error", "name", name, "err", err)
		}
	}
	m.servers = make(map[string]*Client)
	m.extMap = make(map[string]*Client)
}

// fileURI converts an absolute filesystem path to a file:// URI.
func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && len(path) >= 2 && path[1] == ':' {
		path = "/" + path
	}
	return "file://" + url.PathEscape(path)
}

// FileURIFromPath converts a path to a file URI (exported for tools).
func FileURIFromPath(path string) string { return fileURI(path) }
