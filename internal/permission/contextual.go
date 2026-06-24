package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/scottymacleod/agentharness/internal/tool"
)

// ContextualGate wraps a base Gate and applies stateful security rules that
// react to what has happened in the session so far. It implements both the
// engine.Gate interface (pre-execution check) and hooks.Hook interface
// (post-execution state update).
//
// Rules:
//   - EgressThenWrite: after any CapNetwork call succeeds, subsequent CapWrite
//     calls require Ask (approval) instead of auto-Allow. Protects against
//     data exfiltration patterns (read sensitive data, network out, then
//     write/encode).
//   - NetworkAllowList: CapNetwork calls are checked against an allowlist of
//     domains. Calls to unlisted domains are denied.
type ContextualGate struct {
	base            Gate
	mu              sync.Mutex
	networkUsed     bool              // true after any CapNetwork call succeeds
	egressThenWrite bool              // enable the egress→write rule
	allowList       map[string]bool   // normalized domain allowlist; nil = no restriction
	onDecision      func(ContextualDecision) // optional observer (for audit)
}

// ContextualDecision records a policy decision for audit/observability.
type ContextualDecision struct {
	Tool     string   `json:"tool"`
	Cap      string   `json:"cap"`
	Rule     string   `json:"rule"`
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason"`
}

// ContextualOpts configures the contextual gate.
type ContextualOpts struct {
	// EgressThenWrite enables the rule: after network egress, writes require
	// approval.
	EgressThenWrite bool
	// NetworkAllowList restricts CapNetwork calls to these domains. An empty
	// list means no restriction. Domains are matched case-insensitively; a
	// leading dot matches subdomains (e.g. ".example.com" matches
	// "api.example.com").
	NetworkAllowList []string
	// OnDecision is called for each contextual rule evaluation (for audit).
	OnDecision func(ContextualDecision)
}

// NewContextualGate wraps base with stateful security rules.
func NewContextualGate(base Gate, opts ContextualOpts) *ContextualGate {
	var allowList map[string]bool
	if len(opts.NetworkAllowList) > 0 {
		allowList = make(map[string]bool, len(opts.NetworkAllowList))
		for _, d := range opts.NetworkAllowList {
			allowList[strings.ToLower(strings.TrimSpace(d))] = true
		}
	}
	return &ContextualGate{
		base:            base,
		egressThenWrite: opts.EgressThenWrite,
		allowList:       allowList,
		onDecision:      opts.OnDecision,
	}
}

// Check implements engine.Gate. It applies contextual rules on top of the base
// gate's decision.
func (g *ContextualGate) Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string) {
	// Base gate first — if it denies, contextual rules don't matter.
	allowed, reason := g.base.Check(ctx, t, input)
	if !allowed {
		return false, reason
	}

	cap := t.Capability()

	// Network allowlist rule.
	if cap == tool.CapNetwork && g.allowList != nil {
		if !g.domainAllowed(input) {
			reason := fmt.Sprintf("%s blocked: destination not in network allowlist", t.Name())
			g.emitDecision(ContextualDecision{
				Tool: t.Name(), Cap: string(cap),
				Rule: "network_allowlist", Decision: Deny, Reason: reason,
			})
			return false, reason
		}
		g.emitDecision(ContextualDecision{
			Tool: t.Name(), Cap: string(cap),
			Rule: "network_allowlist", Decision: Allow, Reason: "domain in allowlist",
		})
	}

	// Egress-then-write rule.
	if g.egressThenWrite && cap == tool.CapWrite {
		g.mu.Lock()
		networkWasUsed := g.networkUsed
		g.mu.Unlock()
		if networkWasUsed {
			reason := fmt.Sprintf("%s requires approval: write after network egress", t.Name())
			g.emitDecision(ContextualDecision{
				Tool: t.Name(), Cap: string(cap),
				Rule: "egress_then_write", Decision: Ask, Reason: reason,
			})
			if g.base.Approver.Approve(ctx, t.Name(), reason) {
				return true, ""
			}
			return false, reason
		}
	}

	return true, ""
}

// PreToolUse implements hooks.Hook (no-op; checking is done in Check).
func (g *ContextualGate) PreToolUse(_ context.Context, _ string, _ json.RawMessage) error {
	return nil
}

// PostToolUse implements hooks.Hook. It updates internal state after a
// successful tool call.
func (g *ContextualGate) PostToolUse(_ context.Context, toolName string, input json.RawMessage, _ string, isError bool) {
	if isError {
		return
	}
	// Determine the capability from the tool name. We don't have a direct
	// reference to the tool here, so we use a heuristic based on known
	// network tools. The engine calls PostToolUse after execution, so we
	// can track state changes.
	if isNetworkTool(toolName) {
		g.mu.Lock()
		g.networkUsed = true
		g.mu.Unlock()
	}
}

// Reset clears the contextual state (e.g. between sessions).
func (g *ContextualGate) Reset() {
	g.mu.Lock()
	g.networkUsed = false
	g.mu.Unlock()
}

// NetworkUsed reports whether a network egress has occurred in this session.
func (g *ContextualGate) NetworkUsed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.networkUsed
}

// domainAllowed checks whether the tool input contains a URL whose domain is
// in the allowlist. If the URL can't be parsed, the call is denied.
func (g *ContextualGate) domainAllowed(input json.RawMessage) bool {
	domain := extractDomain(input)
	if domain == "" {
		return false
	}
	domain = strings.ToLower(domain)

	// Exact match.
	if g.allowList[domain] {
		return true
	}
	// Subdomain match: ".example.com" matches "api.example.com".
	for d := range g.allowList {
		if strings.HasPrefix(d, ".") && strings.HasSuffix(domain, d) {
			return true
		}
	}
	return false
}

// extractDomain tries to extract a domain from common tool input shapes:
// {"url": "..."} or {"query": "..."} (for search tools, which always hit
// their own service).
func extractDomain(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var args struct {
		URL   string `json:"url"`
		Query string `json:"query"`
	}
	if json.Unmarshal(input, &args) != nil {
		return ""
	}
	// Search tools don't have a user-controlled destination.
	if args.URL == "" && args.Query != "" {
		return "search.api"
	}
	if args.URL == "" {
		return ""
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (g *ContextualGate) emitDecision(d ContextualDecision) {
	if g.onDecision != nil {
		g.onDecision(d)
	}
}

// isNetworkTool returns true for known network-egress tools.
func isNetworkTool(name string) bool {
	switch name {
	case "fetch", "web_fetch", "search", "web_search":
		return true
	default:
		return false
	}
}
