package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"github.com/fiddler110/aegis/internal/redact"
)

// redactionAdapter runs redact.Text over an outbound request's user-visible
// content before handing it to base — the outbound half of P81.5/FIND-05.
// internal/security's RedactSecrets (engine.Options.RedactSecrets) already
// scrubs a CapRead tool's *result* before it enters the conversation; this is
// the last-line backstop that covers everything RedactSecrets does not: the
// system prompt, every message regardless of which tool produced it, and —
// the gap RedactSecrets structurally cannot close, since it only ever sees a
// tool's result — a tool *call*'s own arguments, which the model composes
// itself and which can echo a credential read earlier in the same turn.
type redactionAdapter struct {
	base     Adapter
	provider string
	logger   *slog.Logger
}

// WithRedaction wraps base so every outbound request is scrubbed for
// high-confidence secret patterns (internal/redact's class set — PEM keys,
// AWS/GitHub/Slack tokens, JWTs, bearer tokens, key/secret/password
// assignments) before it leaves the process, and logs a SHA-256 hash plus
// byte count of the (post-redaction) payload so "what left this machine" is
// answerable after the fact even when nothing matched. providerName names the
// backend for that log line. Returns base unchanged when base is nil.
//
// Callers decide *when* to wrap — providerfactory.decorate applies this only
// when the resolved endpoint is not loopback and
// security.redact_outbound_payloads (on by default) hasn't been turned off,
// per config.IsLoopbackBaseURL. A local Ollama deployment pays neither the
// redaction pass nor its false-positive risk, since nothing left the machine
// to redact from in the first place.
func WithRedaction(base Adapter, providerName string, logger *slog.Logger) Adapter {
	if base == nil {
		return base
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &redactionAdapter{base: base, provider: providerName, logger: logger}
}

func (a *redactionAdapter) Name() string    { return a.base.Name() }
func (a *redactionAdapter) Unwrap() Adapter { return a.base }

// IsRedacted reports whether a's decorator chain includes WithRedaction —
// the plumbing guard providerfactory's tests use to check the loopback gate
// wired the layer in (or left it out) as intended, mirroring AdmissionDepth's
// walk-the-Unwrap-chain shape.
func IsRedacted(a Adapter) bool {
	for a != nil {
		if _, ok := a.(*redactionAdapter); ok {
			return true
		}
		u, ok := a.(interface{ Unwrap() Adapter })
		if !ok {
			return false
		}
		a = u.Unwrap()
	}
	return false
}

func (a *redactionAdapter) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	req.System, _ = redact.Text(req.System)
	req.Messages = redactMessages(req.Messages)

	if b, err := json.Marshal(req); err == nil {
		sum := sha256.Sum256(b)
		a.logger.Info("outbound provider payload",
			"provider", a.provider, "bytes", len(b), "sha256", hex.EncodeToString(sum[:]))
	}
	return a.base.Stream(ctx, req)
}

// redactMessages returns a copy of msgs with redact.Text applied to every
// text-bearing block. It copies rather than mutates in place: msgs is the
// engine's live conversation slice, shared across retries and, on the daemon,
// persisted after the request returns — redacting it in place would persist
// "[redacted: ...]" placeholders into session history in place of what the
// model actually said or read.
func redactMessages(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		content := make([]Block, len(m.Content))
		for j, b := range m.Content {
			content[j] = redactBlock(b)
		}
		out[i] = Message{Role: m.Role, Content: content}
	}
	return out
}

// redactBlock redacts the one field of b that can carry free-form text
// reaching the model verbatim. ImageBlock and ThinkingBlock pass through
// unchanged: an image is binary data redact.Text cannot meaningfully inspect,
// and a ThinkingBlock's Signature must round-trip byte-for-byte (see its own
// doc comment) — redacting Text alone, never Signature, keeps that contract.
func redactBlock(b Block) Block {
	switch v := b.(type) {
	case TextBlock:
		v.Text, _ = redact.Text(v.Text)
		return v
	case ToolResultBlock:
		v.Content, _ = redact.Text(v.Content)
		return v
	case ToolUseBlock:
		return redactToolUse(v)
	default:
		return b
	}
}

// redactToolUse redacts a tool call's own arguments — the gap RedactSecrets
// cannot close (P81.5's "tool arguments" half). Input is JSON, and
// redact.Text operates as opaque text substitution, so a match can straddle a
// quote delimiter and leave invalid JSON (internal/hooks.auditInput hits the
// same failure mode for the audit record). There the fix is to re-wrap the
// result as a JSON string; here it is not: a provider's tool_use.input must
// stay whatever type the schema declared (almost always an object), and
// silently changing it would break the request rather than merely look
// unusual in a log. So an invalid-JSON result is discarded and the original,
// unredacted Input is sent instead — the same conservative direction
// TruncateHead's posture table takes when a transform can't be applied safely
// (favor the request working over a redaction guarantee for this one field).
func redactToolUse(v ToolUseBlock) ToolUseBlock {
	if len(v.Input) == 0 {
		return v
	}
	redacted, n := redact.Text(string(v.Input))
	if n == 0 || !json.Valid([]byte(redacted)) {
		return v
	}
	v.Input = json.RawMessage(redacted)
	return v
}
