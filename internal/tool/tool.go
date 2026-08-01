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
	"strings"
	"sync"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// Result is the outcome of executing a tool.
type Result struct {
	Content string // text returned to the model
	IsError bool   // true if the tool failed in a way the model should see
}

// Capability classifies the kind of access a tool needs, so permission modes
// can gate tools by risk without knowing each tool individually.
type Capability string

const (
	CapRead    Capability = "read"    // reads local files/state, no mutation
	CapWrite   Capability = "write"   // mutates local files/state
	CapExecute Capability = "execute" // runs arbitrary commands
	CapNetwork Capability = "network" // makes outbound network requests
	CapSpawn   Capability = "spawn"   // launches a sub-agent (delegation)
)

// Tool is a capability the model can invoke.
type Tool interface {
	// Name is the unique tool identifier exposed to the model.
	Name() string
	// Description tells the model when and how to use the tool.
	Description() string
	// InputSchema is the JSON Schema for the tool's arguments.
	InputSchema() json.RawMessage
	// Capability reports the access class of the tool, used for gating.
	Capability() Capability
	// Execute runs the tool with the given JSON arguments.
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

// OutputSchemer is an optional extension a Tool may implement to declare a
// typed JSON Schema for its return value (P3.6). The registry emits the schema
// alongside the input schema so clients and validators can handle structured
// results without string-parsing.
type OutputSchemer interface {
	OutputSchema() json.RawMessage
}

// CapabilityOverrider is an optional Tool extension for a tool whose
// effective capability depends on the specific call rather than being fixed
// per tool (P25.4c). The shell tool is the motivating case: most invocations
// are CapExecute, but a narrow read-only subset (ls, cat, git status/log/
// diff, …) should be gated as CapRead instead — gating every invocation as
// execute forces a full approval prompt even for commands that mutate
// nothing.
type CapabilityOverrider interface {
	CapabilityFor(input json.RawMessage) Capability
}

// EffectiveCapability returns the capability that should gate a specific
// call: t's static Capability(), unless t implements CapabilityOverrider and
// reports a different capability for this input.
func EffectiveCapability(t Tool, input json.RawMessage) Capability {
	if o, ok := t.(CapabilityOverrider); ok {
		return o.CapabilityFor(input)
	}
	return t.Capability()
}

// Registry holds registered tools and tracks which are exposed.
type Registry struct {
	mu          sync.RWMutex
	tools       map[string]Tool
	exposed     map[string]bool
	deferred    map[string]bool       // tool is known but loaded on demand via tool_search
	schemaCache []provider.ToolSchema // nil means dirty; rebuilt on next Schemas call
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:    map[string]Tool{},
		exposed:  map[string]bool{},
		deferred: map[string]bool{},
	}
}

// Info is a lightweight name+description pair used to advertise deferred tools
// in the system prompt without shipping their full schema.
type Info struct {
	Name        string
	Description string
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
	r.schemaCache = nil
	return nil
}

// Upsert adds or replaces a tool and ensures it is exposed. Unlike Register it
// never returns an error and is safe to call for tools that already exist.
func (r *Registry) Upsert(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	r.tools[name] = t
	r.exposed[name] = true
	r.schemaCache = nil
}

// SetExposed toggles whether a registered tool is offered to the model.
func (r *Registry) SetExposed(name string, exposed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		r.exposed[name] = exposed
		r.schemaCache = nil
	}
}

// RegisterDeferred adds a tool that is known to the harness but not exposed to
// the model until loaded on demand (P4.6). Deferred tools appear only as a
// name+description line in the system prompt; the tool_search meta-tool exposes
// them when a task needs them. This keeps per-turn schema tokens low.
func (r *Registry) RegisterDeferred(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	r.exposed[name] = false
	r.deferred[name] = true
	r.schemaCache = nil
	return nil
}

// Deferred returns the name+description of every deferred tool that has not yet
// been loaded (exposed). Used to build the system-prompt advertisement.
func (r *Registry) Deferred() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Info
	for name, t := range r.tools {
		if r.deferred[name] && !r.exposed[name] {
			out = append(out, Info{Name: t.Name(), Description: t.Description()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Load exposes previously-deferred tools by name so their schemas are offered on
// the next turn. Unknown names are ignored. Returns the tools that were newly
// exposed.
func (r *Registry) Load(names ...string) []Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	var loaded []Tool
	for _, n := range names {
		t, ok := r.tools[n]
		if !ok {
			continue
		}
		if !r.exposed[n] {
			r.exposed[n] = true
			r.schemaCache = nil
		}
		loaded = append(loaded, t)
	}
	return loaded
}

// SearchDeferred returns deferred, not-yet-loaded tools whose name or
// description contains any of the given lowercase query terms. An empty query
// matches all deferred tools.
func (r *Registry) SearchDeferred(query string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	terms := strings.Fields(strings.ToLower(query))
	var out []Tool
	for name, t := range r.tools {
		if !r.deferred[name] || r.exposed[name] {
			continue
		}
		if len(terms) == 0 {
			out = append(out, t)
			continue
		}
		hay := strings.ToLower(t.Name() + " " + t.Description())
		for _, term := range terms {
			if strings.Contains(hay, term) {
				out = append(out, t)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Clone returns a lightweight copy of the registry for session-scoped tool
// exposure (P9): the underlying tool set (registration) is shared by
// reference — a tool registered or dynamically upserted (e.g. MCP's
// tools/list_changed refresh) on the original is visible through every
// clone — but the exposed/deferred maps are independent copies. Calling Load
// (via tool_search) on a clone only exposes a tool for whoever holds that
// clone, instead of the process-global Registry a session's tool_search call
// previously mutated permanently for every other concurrent or future
// session and persona.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exposed := make(map[string]bool, len(r.exposed))
	for k, v := range r.exposed {
		exposed[k] = v
	}
	deferred := make(map[string]bool, len(r.deferred))
	for k, v := range r.deferred {
		deferred[k] = v
	}
	return &Registry{
		tools:    r.tools,
		exposed:  exposed,
		deferred: deferred,
	}
}

// registryCtxKey is the context key for the live registry a tool call is
// executing against.
type registryCtxKey struct{}

// WithRegistry returns a context carrying the registry a tool call is running
// against — the same one Schemas() used to build the tool list the model was
// offered this turn. A meta-tool like tool_search reads this via
// RegistryFromContext to mutate exposure state on the caller's actual
// (possibly session-scoped) registry, rather than one fixed at the tool's own
// construction time.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryCtxKey{}, r)
}

// RegistryFromContext returns the registry carried by ctx, if any.
func RegistryFromContext(ctx context.Context) (*Registry, bool) {
	r, ok := ctx.Value(registryCtxKey{}).(*Registry)
	return r, ok
}

// workdirCtxKey is the context key for a per-call working-directory override.
type workdirCtxKey struct{}

// WithWorkdir returns a context carrying the working directory a tool call
// should be confined to, overriding the tool's own construction-time root
// (P25.1). This lets a single daemon-wide Registry — with its MCP
// connections, plugins, and swarm backend wired once — serve sessions rooted
// at different directories without rebuilding any of that per session: each
// workspace-confined tool reads this value at Execute time (via
// WorkdirFromContext) instead of only ever using its baked-in root.
func WithWorkdir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workdirCtxKey{}, dir)
}

// WorkdirFromContext returns the working directory carried by ctx, if any.
// ok is false when unset or empty, so callers can fall back to their own
// default root with a single conditional.
func WorkdirFromContext(ctx context.Context) (string, bool) {
	dir, _ := ctx.Value(workdirCtxKey{}).(string)
	return dir, dir != ""
}

// extraRootsCtxKey is the context key for the session's additional
// confinement roots.
type extraRootsCtxKey struct{}

// WithExtraRoots returns a context carrying the additional roots
// (workspace.additional_roots, P52.13) that workspace-confined tools may
// resolve paths into, on top of the workdir carried by WithWorkdir.
//
// It rides the context for the same reason the workdir does: one daemon-wide
// Registry serves sessions rooted at different directories, so the root set is
// a property of the call, not of the tool instance. The workdir stays a
// separate value rather than becoming roots[0] because plenty of callers want
// only the working directory (a shell cwd, a repo-map anchor) and have no
// business seeing the wider set.
func WithExtraRoots(ctx context.Context, roots []sandbox.Root) context.Context {
	if len(roots) == 0 {
		return ctx
	}
	return context.WithValue(ctx, extraRootsCtxKey{}, roots)
}

// ExtraRootsFromContext returns the additional confinement roots carried by
// ctx, or nil when the session has none — the overwhelmingly common case, and
// the one that reproduces exactly the single-root behavior.
func ExtraRootsFromContext(ctx context.Context) []sandbox.Root {
	roots, _ := ctx.Value(extraRootsCtxKey{}).([]sandbox.Root)
	return roots
}

// Get returns a registered tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool, regardless of exposure/deferred state.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Schemas returns provider tool schemas for all exposed tools, sorted by name.
// Results are cached until the registry is mutated.
func (r *Registry) Schemas() []provider.ToolSchema {
	r.mu.RLock()
	if r.schemaCache != nil {
		out := r.schemaCache
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.schemaCache != nil { // double-check after acquiring write lock
		return r.schemaCache
	}
	out := make([]provider.ToolSchema, 0, len(r.tools))
	for name, t := range r.tools {
		if !r.exposed[name] {
			continue
		}
		ts := provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		}
		if os, ok := t.(OutputSchemer); ok {
			ts.OutputSchema = os.OutputSchema()
		}
		out = append(out, ts)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	r.schemaCache = out
	return out
}
