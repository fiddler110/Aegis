// The daemon's interactive approval seam: an engine-side Approver that turns
// a permission Ask into an SSE event and blocks on the operator's answer.
// Extracted from server.go (L4).
package server

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/fiddler110/aegis/internal/api"
)

// approvalDecision carries the client's answer to an interactive approval prompt.
type approvalDecision struct {
	Approved    bool
	AllowAlways bool
	Pattern     string // non-empty: persist "allow tool(pattern)" instead of caching per-tool (TQ6)
}

// sseApprover implements permission.Approver by sending a KindApprovalRequest
// SSE event and blocking until the client POSTs a /sessions/{id}/approve
// answer for that specific call.
//
// Each call registers its own id and its own answer channel in
// pendingApprovals, rather than sharing one channel keyed by the run
// (P81.33/FIND-33). A parallel tool round can have more than one call inside
// Approve at once — up to maxParallelTools goroutines, all reaching the
// permission gate independently — and a single run-scoped channel meant
// whichever of them read first got whatever decision the operator had just
// sent, correct or not: the second call's dialog silently clobbered the
// first's in the TUI, and the first call's goroutine then hung until the run
// ended. Per-call ids and channels make every answer land on the call it was
// actually sent for. AllowAlways decisions are stored in permCache so future
// calls to the same tool within the session are auto-approved without
// prompting.
type sseApprover struct {
	send      func(api.Event)
	runID     string
	sessionID string
	permCache *sync.Map // key: sessionID+"\x00"+toolName → struct{}

	// pendingApprovals is the server's registry of per-call answer channels;
	// handleApprove reads from it by the id an ApproveRequest echoes back.
	// Shared with every sseApprover the daemon runs, keyed by the globally
	// unique per-call id nextID mints — never by runID alone, which is only
	// this id's prefix.
	pendingApprovals *sync.Map

	// persistRule installs a pattern-scoped "allow tool(pattern)" permission
	// rule when the client answers allow-always with a pattern (TQ6). May be
	// nil (e.g. tests), in which case the per-tool cache is used instead.
	persistRule func(toolName, pattern string)

	mu      sync.Mutex
	seq     int
	pending []api.ApprovalItem // calls currently awaiting an answer, oldest first
}

// nextID mints a call id unique within this run: the run id plus a
// per-approver sequence number.
func (a *sseApprover) nextID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	return a.runID + "-" + strconv.Itoa(a.seq)
}

func (a *sseApprover) Approve(ctx context.Context, toolName, reason string, input json.RawMessage) bool {
	// Check session-scoped allow-always cache before prompting.
	cacheKey := a.sessionID + "\x00" + toolName
	if _, ok := a.permCache.Load(cacheKey); ok {
		return true
	}

	id := a.nextID()
	ch := make(chan approvalDecision, 1)
	a.pendingApprovals.Store(id, ch)
	defer a.pendingApprovals.Delete(id)

	item := api.ApprovalItem{ID: id, Tool: toolName, Input: input, Reason: reason}
	a.mu.Lock()
	a.pending = append(a.pending, item)
	batch := a.batchSnapshotLocked()
	a.mu.Unlock()

	a.send(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           toolName,
		ToolInput:      input,
		ApprovalReason: reason,
		ApprovalID:     id,
		ApprovalBatch:  batch,
	})

	var d approvalDecision
	select {
	case d = <-ch:
	case <-ctx.Done():
		d = approvalDecision{Approved: false}
	}

	a.mu.Lock()
	a.removeLocked(id)
	a.mu.Unlock()

	if d.AllowAlways && d.Approved {
		// A pattern-scoped rule beats the whole-tool cache: approving
		// "npm test*" must not silently approve every future shell call.
		if d.Pattern != "" && a.persistRule != nil {
			a.persistRule(toolName, d.Pattern)
		} else {
			a.permCache.Store(cacheKey, struct{}{})
		}
	}
	return d.Approved
}

// batchSnapshotLocked copies the currently-pending set for one outgoing
// event. Called with a.mu held. Nil when this call is the only one pending —
// a client that never learns to read ApprovalBatch still renders correctly
// off the single-item fields the event always carries.
func (a *sseApprover) batchSnapshotLocked() []api.ApprovalItem {
	if len(a.pending) <= 1 {
		return nil
	}
	items := make([]api.ApprovalItem, len(a.pending))
	copy(items, a.pending)
	return items
}

func (a *sseApprover) removeLocked(id string) {
	for i, it := range a.pending {
		if it.ID == id {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return
		}
	}
}
