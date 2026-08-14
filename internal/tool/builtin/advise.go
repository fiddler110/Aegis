package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
)

// adviseTool is P13.4's Nebula-inspired security engagement tooling:
// security_advise. It is one action-style tool (mirroring dast_scan's "mode"
// field for selecting sub-operation, generalized here to an "action" enum
// since this tool covers four sub-scopes rather than one) rather than four
// separate tools, per the P13.4 roadmap item's own framing as a single
// piece of engagement tooling.
//
// Capability is CapNetwork: the tool's real risk surface is outbound egress
// for cve_lookup (against NVD, a fixed public host — not model/attacker
// supplied, so this needs none of web_fetch's SSRF dialer). The notebook
// actions (note/list/suggest/status) are local file reads/appends under a
// dedicated per-engagement store (see internal/security/notebook.go) —
// bookkeeping in the same low-risk vein as the always-CapWrite `remember`
// tool, not a workspace mutation, so folding them under CapNetwork rather
// than forcing every call through the higher-friction write-capability path
// matches how this tool is actually used: an operator/model narrating an
// ongoing engagement and occasionally looking something up, not repeatedly
// executing anything.
//
// Note action fully absent from this tool: it never auto-runs recon_scan,
// dast_scan, or any other tool on the suggest action's output — see
// SuggestNextSteps' doc comment for why that's "guarded" by design.
type adviseTool struct {
	// root is the daemon's per-user data directory (Options.DataDir); the
	// engagement notebook survives there across projects/sessions/days, the
	// same seam internal/longmem and internal/knowledge use for
	// cross-session state (see notebook.go's storage-choice comment).
	root string
}

func (t *adviseTool) Name() string                { return "security_advise" }
func (t *adviseTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *adviseTool) Description() string {
	return "Security engagement assistant (P13.4): a persistent, multi-day engagement notebook plus CVE lookups and guarded next-step suggestions, scoped to a named \"engagement\" string that survives across sessions and daemon restarts (not the ephemeral chat session). Actions: \"note\" appends a timestamped note (optionally tagged, e.g. [\"recon\",\"finding\"]) to the named engagement's notebook. \"list\" (alias \"log\") returns every note recorded for that engagement, oldest first. \"cve_lookup\" queries the NVD REST API by exactly one of: CVE ID (cve_id, e.g. \"CVE-2021-44228\"); a CPE-based product+version match (product and version together, e.g. product=\"apache_http_server\" version=\"2.4.29\" — prefer this whenever a scanner captured a versioned service banner, e.g. from recon_scan's nmap pass, since it matches NVD's affected-product field instead of free-text prose and won't return an unrelated CVE that merely shares vocabulary); or free-text keyword (keyword) — a weaker fallback for findings with no captured version (e.g. a misconfiguration template with no service banner). The unauthenticated public API is rate-limited to roughly 5 requests/30s and a 403/429 is returned as a clear error, not a hang; set NVD_API_KEY in the environment for a higher limit. \"suggest\" returns plain-text, rule-based next-step suggestions derived from the engagement's own notebook content (e.g. no recon logged yet, or findings referenced but never documented) — this NEVER executes another tool or scan itself, it only proposes; a human or the calling model decides whether to act. \"status\" returns a short digest of the engagement's notebook (note count, date range, and how many notes reference recon/dast/security_scan/findings/cve lookups)."
}

// ShortDescription is the deferred-tools advertisement (P62.6). The derived
// fallback would open with "Security engagement assistant (P13.4):", leaking an
// internal roadmap id into the system prompt as the first thing the model reads
// about this tool.
func (t *adviseTool) ShortDescription() string {
	return "Persistent security-engagement notebook: record and list notes, look up CVEs by ID/product/keyword, and suggest next steps."
}
func (t *adviseTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"action":{"type":"string","enum":["note","list","log","cve_lookup","suggest","status"],"description":"which sub-operation to run"},"engagement":{"type":"string","description":"named engagement the notebook is scoped to; required for note/list/log/suggest/status"},"text":{"type":"string","description":"note text (action: note)"},"tags":{"type":"array","items":{"type":"string"},"description":"optional labels for the note, e.g. [\"recon\",\"finding\"] (action: note)"},"cve_id":{"type":"string","description":"a specific CVE ID to look up, e.g. \"CVE-2021-44228\" (action: cve_lookup; mutually exclusive with keyword and product/version)"},"product":{"type":"string","description":"product name for a CPE-based match, e.g. \"apache_http_server\" (action: cve_lookup; requires version; prefer this over keyword when a scanner captured a versioned service banner; mutually exclusive with cve_id and keyword)"},"version":{"type":"string","description":"version string for a CPE-based match, e.g. \"2.4.29\" (action: cve_lookup; requires product)"},"keyword":{"type":"string","description":"free-text NVD keyword search — weaker fallback when no product/version is known (action: cve_lookup; mutually exclusive with cve_id and product/version)"},"limit":{"type":"integer","description":"max results for a keyword or product/version search (default 5, max 20)"}},"required":["action"]}`)
}

func (t *adviseTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Action     string   `json:"action"`
		Engagement string   `json:"engagement"`
		Text       string   `json:"text"`
		Tags       []string `json:"tags"`
		CVEID      string   `json:"cve_id"`
		Product    string   `json:"product"`
		Version    string   `json:"version"`
		Keyword    string   `json:"keyword"`
		Limit      int      `json:"limit"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}

	root := t.root
	if root == "" {
		root = "."
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "note":
		if strings.TrimSpace(args.Engagement) == "" {
			return tool.Result{Content: "engagement is required for action \"note\"", IsError: true}, nil
		}
		if err := security.NotebookAppend(root, args.Engagement, args.Text, args.Tags); err != nil {
			return tool.Result{Content: fmt.Sprintf("could not save note: %v", err), IsError: true}, nil
		}
		return tool.Result{Content: fmt.Sprintf("noted for engagement %q", args.Engagement)}, nil

	case "list", "log":
		if strings.TrimSpace(args.Engagement) == "" {
			return tool.Result{Content: "engagement is required for action \"" + args.Action + "\"", IsError: true}, nil
		}
		entries, err := security.NotebookList(root, args.Engagement)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("could not read notebook: %v", err), IsError: true}, nil
		}
		return tool.Result{Content: formatNotebookEntries(args.Engagement, entries)}, nil

	case "cve_lookup":
		records, err := security.LookupCVE(ctx, security.CVEOptions{CVEID: args.CVEID, Product: args.Product, Version: args.Version, Keyword: args.Keyword, Limit: args.Limit})
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("cve_lookup failed: %v", err), IsError: true}, nil
		}
		return tool.Result{Content: security.FormatCVEResults(records)}, nil

	case "suggest":
		if strings.TrimSpace(args.Engagement) == "" {
			return tool.Result{Content: "engagement is required for action \"suggest\"", IsError: true}, nil
		}
		entries, err := security.NotebookList(root, args.Engagement)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("could not read notebook: %v", err), IsError: true}, nil
		}
		suggestions := security.SuggestNextSteps(entries)
		var b strings.Builder
		for _, s := range suggestions {
			b.WriteString("- ")
			b.WriteString(s)
			b.WriteString("\n")
		}
		return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil

	case "status":
		// Status digest (P13.4.4): a tool-action fallback rather than
		// wiring into api.StatusInfo/GET /status. That endpoint is
		// deliberately daemon-global with no precedent for a keyed/
		// parameterized field (every existing field — provider, model,
		// daily cost, agent concurrency — is a single daemon-wide value),
		// and it's polled frequently (waitForDaemon-style) without query
		// params today. Threading a per-engagement key through it would be
		// a bigger, differently-shaped change than this digest is worth;
		// the same information is one tool call away here instead.
		if strings.TrimSpace(args.Engagement) == "" {
			return tool.Result{Content: "engagement is required for action \"status\"", IsError: true}, nil
		}
		entries, err := security.NotebookList(root, args.Engagement)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("could not read notebook: %v", err), IsError: true}, nil
		}
		digest := security.DigestNotebook(args.Engagement, entries)
		return tool.Result{Content: digest.Format()}, nil

	default:
		return tool.Result{Content: fmt.Sprintf("unknown action %q (want note, list, log, cve_lookup, suggest, or status)", args.Action), IsError: true}, nil
	}
}

func formatNotebookEntries(engagement string, entries []security.NotebookEntry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("no notes recorded yet for engagement %q", engagement)
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] %s", e.Time.Format("2006-01-02 15:04"), e.Text)
		if len(e.Tags) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(e.Tags, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
