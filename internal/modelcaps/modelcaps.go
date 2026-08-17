// Package modelcaps persists what Aegis has learned the hard way about a
// model's capabilities, so a restart doesn't re-pay the discovery (P53.5).
//
// Aegis probes rather than declares: the native Ollama adapter finds out that a
// model 400s on `think` by sending one and being rejected
// (internal/provider/ollama), and internal/toolcallprobe finds out whether a
// model can emit a structured tool call by asking it to. Both verdicts lived in
// process memory only, so every daemon restart re-sent the doomed request and
// re-ran the probe. This package is the small on-disk cache that lets a probe
// happen once.
//
// Three rules shape it, and each is load-bearing:
//
//   - **Cache, never source of truth.** Deleting the file must only ever cost a
//     re-probe. Nothing here can wedge a model into a capability it doesn't have:
//     a record is consulted to *skip* work already known to fail, and any read
//     that finds nothing simply falls through to the live discovery path.
//   - **Staleness is real.** An Ollama tag is mutable — `ollama pull` can replace
//     what "qwen3:14b" means without the name changing — which is exactly why
//     toolcallprobe.Gate refused to persist its verdicts. Reconcile answers that:
//     the daemon hands the store the current per-model digests, every record
//     whose fingerprint moved is dropped, and new records are stamped with the
//     digest they were measured against.
//   - **User-declared outranks discovered.** A user who already knows a model's
//     quirk can declare it (provider.model_capabilities) and be believed without
//     the probe ever meeting that model. Precedence is
//     declared > persisted-discovered > default (unknown, discover it live).
package modelcaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// FileName is the store's basename under the data dir. Exported so `aegis
// doctor` can name the file it is telling the user is safe to delete.
const FileName = "model_caps.json"

// Path returns the store's location under dataDir.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// ToolCalling is a persisted tool-calling conformance sample: the P53.4
// Conformance counters plus the verdict they produced, flattened to plain ints
// so this package doesn't import toolcallprobe (which must be free to import
// this one).
//
// The counters, not just the verdict, are what gets stored. A verdict is one
// bit; the rate is the thing that tells a later run whether a model complies
// 100% or 60% of the time, which is the difference between a drive that
// finishes and one that fails looking like a harness bug.
type ToolCalling struct {
	// Verdict is "ok" or "unsupported". Never "unknown": a verdict the probe
	// couldn't justify is not persisted at all (see Store.SetToolCalling).
	Verdict string `json:"verdict"`
	// Trials is how many probe trials produced a result.
	Trials int `json:"trials"`
	// ToolCallTrials is how many of those emitted at least one tool call.
	ToolCallTrials int `json:"tool_call_trials"`
	// NoVerdict is how many hit the token cap before answering; excluded from
	// the rate's denominator, kept so a reader can see the sample was clouded.
	NoVerdict int `json:"no_verdict"`
	// Native records whether the model's own manifest claims native
	// tool-calling support, when that was observable. nil = never determined.
	// Kept alongside the measured verdict rather than merged into it precisely
	// because the two can disagree — a manifest can claim tool support the
	// weights don't deliver, and that disagreement is worth being able to see.
	Native *bool `json:"native,omitempty"`
}

// Rate reports the fraction of verdict-reaching trials that called a tool, and
// whether there is a rate at all. Mirrors toolcallprobe.Conformance.Rate: an
// all-truncated sample has no rate, and reporting 0.0 for it would accuse a
// model the probe never got an answer out of.
func (t ToolCalling) Rate() (float64, bool) {
	d := t.Trials - t.NoVerdict
	if d <= 0 {
		return 0, false
	}
	return float64(t.ToolCallTrials) / float64(d), true
}

// Record is everything known about one model.
type Record struct {
	// Fingerprint is the model digest the record was measured against, when it
	// was known at write time. Empty means "measured before any digest was
	// available" — such a record is kept (dropping it would lose a real
	// measurement) but is refreshed with a fingerprint the first time one is
	// reconciled in.
	Fingerprint string `json:"fingerprint,omitempty"`
	// ThinkRejected latches the P52.5 verdict "this model 400s the instant
	// `think` is sent". Only ever written as true — the adapter can prove
	// rejection, never prove acceptance for all future values — so nil means
	// "not known to reject", not "known to accept".
	ThinkRejected *bool `json:"think_rejected,omitempty"`
	// ToolCalling is the persisted conformance sample, nil until one is taken.
	ToolCalling *ToolCalling `json:"tool_calling,omitempty"`
	// TemplateDropsToolCalls latches whether the model's chat template discards
	// an assistant turn's tool calls when the same turn also carries prose (see
	// ollamainfo.TemplateDropsToolCalls). Unlike ThinkRejected this is written
	// in both directions: it is read off the template rather than inferred from
	// a failure, so "observed fine" is a real verdict worth caching. It is
	// fingerprint-scoped like every other field, so rebuilding the model with a
	// corrected template retires the record.
	TemplateDropsToolCalls *bool `json:"template_drops_tool_calls,omitempty"`
	// UpdatedAt is when this record last changed, for a human reading the file.
	UpdatedAt time.Time `json:"updated_at"`
}

// Declared is a user's pre-declaration of a model's capabilities, from
// provider.model_capabilities. Every field is a pointer: unset means "say
// nothing, let discovery decide", which is not the same as declaring false.
type Declared struct {
	// Think, when set to false, declares that the model does not accept the
	// `think` parameter — the adapter then never sends one, so the 400 the
	// latch exists to stop happening never happens even once. Setting it true
	// declares the opposite and suppresses a stale persisted rejection.
	Think *bool `koanf:"think" json:"think,omitempty"`
	// ToolCalling declares the tool-calling verdict ("ok" or "unsupported"),
	// outranking anything the probe recorded.
	ToolCalling string `koanf:"tool_calling" json:"tool_calling,omitempty"`
}

// Store is the JSON-backed record set. Safe for concurrent use; every mutation
// writes the whole file, which is fine at the scale of "models on this machine".
//
// A Store may be nil. Every method tolerates a nil receiver and behaves as an
// empty, non-persisting store, so callers that were never given one (tests, a
// CLI path with no data dir) need no branch of their own.
type Store struct {
	mu       sync.Mutex
	path     string
	records  map[string]Record
	declared map[string]Declared
	// fingerprints is the digest snapshot from the last Reconcile, used to
	// stamp records written afterwards. Empty until a reconcile happens.
	fingerprints map[string]string
}

// Option configures a Store at open time.
type Option func(*Store)

// WithDeclared installs the user's pre-declared capabilities. Keys are model
// names, matched exactly as the provider spells them.
func WithDeclared(d map[string]Declared) Option {
	return func(s *Store) { s.declared = d }
}

// Open loads the store at path, or starts empty when it is missing or
// unreadable. A corrupt file is not an error worth surfacing: this is a cache,
// and starting over costs a re-probe.
func Open(path string, opts ...Option) *Store {
	s := &Store{path: path, records: map[string]Record{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s.records)
		if s.records == nil {
			s.records = map[string]Record{}
		}
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Reconcile drops every record whose model digest has moved since it was
// written and stamps the store with the current digests so later writes record
// theirs.
//
// A model absent from digests is deliberately left alone rather than dropped:
// absence means "we couldn't ask" (a non-Ollama provider, an unreachable
// server, a model not pulled locally), and discarding a valid measurement on
// missing evidence is the wrong trade — the record can only ever cost a
// re-probe if it turns out to be wrong, and it is re-checked on every start.
//
// Returns the number of records invalidated.
func (s *Store) Reconcile(digests map[string]string) int {
	if s == nil || len(digests) == 0 {
		return 0
	}
	s.mu.Lock()
	s.fingerprints = digests
	dropped := 0
	for model, rec := range s.records {
		cur, known := digests[model]
		if !known || cur == "" {
			continue
		}
		if rec.Fingerprint == "" {
			// Measured before digests were observable. Adopt the current one
			// rather than throwing the measurement away; from here on it is
			// invalidatable like any other.
			rec.Fingerprint = cur
			s.records[model] = rec
			continue
		}
		if rec.Fingerprint != cur {
			delete(s.records, model)
			dropped++
		}
	}
	s.mu.Unlock()
	if dropped > 0 {
		s.save()
	}
	return dropped
}

// Get returns the persisted record for model. The declared overrides are not
// applied here — use ThinkRejected/ToolCallingVerdict for a resolved answer.
func (s *Store) Get(model string) (Record, bool) {
	if s == nil {
		return Record{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[model]
	return rec, ok
}

// Models lists the recorded model names, for diagnostics.
func (s *Store) Models() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.records))
	for m := range s.records {
		out = append(out, m)
	}
	return out
}

// ThinkRejected resolves whether model is known to reject the `think`
// parameter, and whether that is known at all. Declaration wins over the
// persisted latch: a user who declares `think: true` for a model whose stale
// record says otherwise gets `think` sent again, which is what makes a wrong
// cached value recoverable without deleting the file.
func (s *Store) ThinkRejected(model string) (rejected, known bool) {
	if s == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.declared[model]; ok && d.Think != nil {
		return !*d.Think, true
	}
	rec, ok := s.records[model]
	if !ok || rec.ThinkRejected == nil {
		return false, false
	}
	return *rec.ThinkRejected, true
}

// SetThinkRejected persists the latch for model. Only true is ever recorded;
// a false call clears the record's flag, which is how a caller retracts a
// verdict it no longer stands behind.
func (s *Store) SetThinkRejected(model string, rejected bool) {
	if s == nil || model == "" {
		return
	}
	s.mu.Lock()
	rec := s.records[model]
	if rejected {
		v := true
		rec.ThinkRejected = &v
	} else {
		rec.ThinkRejected = nil
	}
	s.stampLocked(model, rec)
	s.mu.Unlock()
	s.save()
}

// TemplateDropsToolCalls reports the persisted verdict for model, and whether
// one exists at all. Both directions are meaningful here — see the Record
// field — so a cached false is a hit, not a miss.
func (s *Store) TemplateDropsToolCalls(model string) (drops, known bool) {
	if s == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[model]
	if !ok || rec.TemplateDropsToolCalls == nil {
		return false, false
	}
	return *rec.TemplateDropsToolCalls, true
}

// SetTemplateDropsToolCalls persists the verdict for model.
func (s *Store) SetTemplateDropsToolCalls(model string, drops bool) {
	if s == nil || model == "" {
		return
	}
	s.mu.Lock()
	rec := s.records[model]
	v := drops
	rec.TemplateDropsToolCalls = &v
	s.stampLocked(model, rec)
	s.mu.Unlock()
	s.save()
}

// ToolCalling resolves model's tool-calling record. A user declaration
// substitutes its verdict while keeping any measured counters visible, so a
// declared override never fabricates a sample it didn't take.
func (s *Store) ToolCalling(model string) (ToolCalling, bool) {
	if s == nil {
		return ToolCalling{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var tc ToolCalling
	var have bool
	if rec, ok := s.records[model]; ok && rec.ToolCalling != nil {
		tc, have = *rec.ToolCalling, true
	}
	if d, ok := s.declared[model]; ok && (d.ToolCalling == "ok" || d.ToolCalling == "unsupported") {
		tc.Verdict = d.ToolCalling
		have = true
	}
	return tc, have
}

// SetToolCalling persists a conformance sample. A sample with no verdict
// ("unknown", or an empty one) is not written at all: the probe's own contract
// is that an unreached verdict must never be cached, and persisting one would
// carry that mistake across restarts instead of just across sessions.
func (s *Store) SetToolCalling(model string, tc ToolCalling) {
	if s == nil || model == "" {
		return
	}
	if tc.Verdict != "ok" && tc.Verdict != "unsupported" {
		return
	}
	s.mu.Lock()
	rec := s.records[model]
	rec.ToolCalling = &tc
	s.stampLocked(model, rec)
	s.mu.Unlock()
	s.save()
}

// ProbeStore adapts a Store to internal/toolcallprobe's Store interface, whose
// method shapes are flattened scalars so that package can stay free of this
// one's types. A nil *Store yields a working no-op, matching Store's own
// nil-tolerance.
type ProbeStore struct{ S *Store }

// ToolCalling implements toolcallprobe.Store.
func (p ProbeStore) ToolCalling(model string) (verdict string, trials, toolCallTrials, noVerdict int, ok bool) {
	tc, ok := p.S.ToolCalling(model)
	if !ok {
		return "", 0, 0, 0, false
	}
	return tc.Verdict, tc.Trials, tc.ToolCallTrials, tc.NoVerdict, true
}

// SetToolCalling implements toolcallprobe.Store, preserving any Native claim
// already recorded — the probe knows nothing about the manifest's advertised
// capability and must not erase it by writing a sample.
func (p ProbeStore) SetToolCalling(model, verdict string, trials, toolCallTrials, noVerdict int) {
	tc := ToolCalling{Verdict: verdict, Trials: trials, ToolCallTrials: toolCallTrials, NoVerdict: noVerdict}
	if rec, ok := p.S.Get(model); ok && rec.ToolCalling != nil {
		tc.Native = rec.ToolCalling.Native
	}
	p.S.SetToolCalling(model, tc)
}

// SetNativeToolSupport records the model manifest's own claim about tool
// support, leaving any measured sample untouched. Written even when no sample
// exists yet — but only as part of a record that will carry one, so a claim
// alone never masquerades as a verdict (ToolCalling's Verdict stays empty,
// which every reader treats as "no verdict").
func (s *Store) SetNativeToolSupport(model string, native bool) {
	if s == nil || model == "" {
		return
	}
	s.mu.Lock()
	rec := s.records[model]
	tc := ToolCalling{}
	if rec.ToolCalling != nil {
		tc = *rec.ToolCalling
	}
	if tc.Native != nil && *tc.Native == native {
		s.mu.Unlock()
		return
	}
	tc.Native = &native
	rec.ToolCalling = &tc
	s.stampLocked(model, rec)
	s.mu.Unlock()
	s.save()
}

// Forget drops model's record. The manual escape hatch behind "this is a cache,
// safe to delete" at single-model granularity.
func (s *Store) Forget(model string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	_, existed := s.records[model]
	delete(s.records, model)
	s.mu.Unlock()
	if existed {
		s.save()
	}
}

// stampLocked applies the current fingerprint and timestamp to rec and stores
// it. Caller holds s.mu.
func (s *Store) stampLocked(model string, rec Record) {
	if fp := s.fingerprints[model]; fp != "" {
		rec.Fingerprint = fp
	}
	rec.UpdatedAt = time.Now().UTC()
	s.records[model] = rec
}

// save writes the whole store. Errors are swallowed on purpose: a cache that
// can't persist still works, and failing a model request because a cache write
// failed would be strictly worse than re-probing next start.
func (s *Store) save() {
	if s == nil || s.path == "" {
		return
	}
	s.mu.Lock()
	data, err := json.MarshalIndent(s.records, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return
	}
	_ = fsguard.RestrictToOwner(s.path)
}
