// Package tool defines the tool abstraction and a registry. The registry
// deliberately separates *registration* (a tool is known to the harness) from
// *exposure* (a tool is offered to the model for a given session/mode), a
// pattern borrowed from Hermes that lets permission modes gate capability
// without unregistering tools.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/scottymacleod/agentharness/internal/provider"
)

// Result is the outcome of executing a tool.
type Result struct {
	Content string // text returned to the model
	IsError bool   // true if the tool failed in a way the model should see
}

// Tool is a capability the model can invoke.
type Tool interface {
	// Name is the unique tool identifier exposed to the model.
	Name() string
	// Description tells the model when and how to use the tool.
	Description() string
	// InputSchema is the JSON Schema for the tool's arguments.
	InputSchema() json.RawMessage
	// Execute runs the tool with the given JSON arguments.
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

// Registry holds registered tools and tracks which are exposed.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	exposed  map[string]bool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:   map[string]Tool{},
		exposed: map[string]bool{},
	}
}

// Register adds a tool. By default a newly registered tool is exposed.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	r.exposed[name] = true
	return nil
}

// SetExposed toggles whether a registered tool is offered to the model.
func (r *Registry) SetExposed(name string, exposed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		r.exposed[name] = exposed
	}
}

// Get returns a registered tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Schemas returns provider tool schemas for all exposed tools, sorted by name.
func (r *Registry) Schemas() []provider.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]provider.ToolSchema, 0, len(r.tools))
	for name, t := range r.tools {
		if !r.exposed[name] {
			continue
		}
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
