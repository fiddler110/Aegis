package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// P64.1: a capped result's remainder used to be unrecoverable. The notice said
// "these are 500 of an unknown number, in no particular order", which is honest
// and is not a recovery path — a model told that has exactly two moves, reason
// from a partial set anyway or re-run a narrower query it has no information to
// narrow correctly, and both are worse than reading the rest.
//
// Spilling writes the full result to a file inside the workspace and names it
// in the truncation notice, so the recovery path is a tool the model already
// has.
//
// # Where the files live, and why not <data_dir>
//
// <data_dir> — alongside builtin-skills/ and model_caps.json — is the obvious
// home and is wrong. Every read tool resolves through resolveRead →
// sandbox.ValidatePathIn over the workspace root plus the session's additional
// roots, and the data dir is under none of them: read_file does not merely
// refuse a path there, it fails with "escapes the workspace root". A locator
// the model cannot open is worse than no locator, so the spill lives under
// <workspace>/.aegis/spill/ instead. TestSpillLocationIsReachableByTheModel
// measures all of this rather than asserting it from a reading of the code.
//
// The same measurement produced one thing the item's write-up assumed and this
// tree cannot deliver: the suggested hint "or grep this path to search within
// it" **does not work**. grep has no path parameter (it always searches the
// workspace root) and ".aegis" is in skipDirNames, so neither search backend
// descends into the spill directory. spillLocator therefore offers read_file
// with offset/limit and does not promise grep. Naming a recovery path that
// silently returns "no matches" is precisely the failure this item removes.
//
// # Session scoping
//
// The item asks for session-scoped files. The tool layer has no session id —
// ctx carries a workdir, extra roots and a registry, and nothing else — and
// adding one to internal/tool is outside this change. The workdir is the
// closest available proxy and is session-scoped in practice (each session gets
// its own), so files are scoped by workspace and bounded by the reaper below
// rather than deleted at session end. That is weaker than the design asks for
// in exactly one way: a spill from a previous session in the same workspace
// stays readable until it ages out.
//
// # Best-effort, always
//
// Every failure path here returns ok=false and leaves the caller's inline
// result exactly as it was. No session, no writable workspace, a full disk or a
// read-only checkout must never turn a successful tool call into an error — the
// spill is an improvement on the notice, never a precondition for it.
const (
	// spillDirRel is workspace-relative. Under .aegis/ so the files are out of
	// the way of glob/grep results (skipDirNames) and of anything a user would
	// commit — which is also why grep cannot reach them; see above.
	spillDirRel = ".aegis/spill"

	// spillTTL is how long a spill file stays useful. A spill is only worth
	// anything while the run that produced it can still reference it, and runs
	// are bounded by MaxWallClockPerRun / MaxTurnStall (900s) rather than by
	// days. 24h is generous against that and short enough that a workspace
	// driven daily does not accumulate a month of them.
	spillTTL = 24 * time.Hour

	// spillMaxFiles and spillMaxBytes bound the directory when files arrive
	// faster than the TTL reaps them — a single long unattended drive can spill
	// hundreds of times in an hour, which the TTL alone would not touch. Oldest
	// first, because the newest spill is the one the current turn's notice
	// points at and deleting it would break the locator we just handed out.
	spillMaxFiles = 200
	spillMaxBytes = 64 << 20 // 64 MiB
)

// spillText writes text to a new file under <root>/.aegis/spill and returns the
// workspace-relative slash path. ok=false means nothing was written and the
// caller must behave exactly as it did before spilling existed.
//
// kind is a short tool-ish label ("shell", "grep") that lands in the file name
// so a human looking at the directory can tell what produced what.
func spillText(ctx context.Context, root, kind, text string) (rel string, ok bool) {
	root = effectiveRoot(ctx, root)
	if root == "" || text == "" {
		return "", false
	}
	dir := filepath.Join(root, filepath.FromSlash(spillDirRel))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", false
	}
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", false
	}
	name := fmt.Sprintf("%s-%s-%s.txt", sanitizeSpillKind(kind), time.Now().UTC().Format("20060102T150405"), hex.EncodeToString(nonce[:]))
	spillPath := filepath.Join(dir, name)
	if err := os.WriteFile(spillPath, []byte(text), 0o600); err != nil {
		return "", false
	}
	// A spill file holds the overflow of a truncated tool result — raw file
	// contents, verbatim. The 0o600 above is sufficient on POSIX and cosmetic
	// on Windows, where a new file inherits its parent directory's ACL and the
	// mode argument does nothing; fsguard applies a real non-inherited ACL
	// there and is a no-op on POSIX. This is the same treatment the daemon
	// token, the three SQLite stores, the TLS key, the swarm mailbox, the trust
	// store and the modelcaps cache already get. Best-effort, deliberately: a
	// spill that cannot be hardened is still better than a truncated result
	// with nowhere to go, and the caller's contract is ok=false means *nothing
	// was written*.
	if err := fsguard.RestrictToOwner(spillPath); err != nil {
		slog.Warn("spill: could not restrict file permissions", "path", spillPath, "err", err)
	}
	reapSpills(dir)
	return spillDirRel + "/" + name, true
}

// sanitizeSpillKind keeps the label to characters that are safe in a filename
// on every platform. The label comes from our own call sites today, but a tool
// name reaching here from a plugin or MCP server would not.
func sanitizeSpillKind(kind string) string {
	var b strings.Builder
	for _, r := range kind {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "result"
	}
	return b.String()
}

// spillLocator is the sentence appended to a truncation notice when the
// remainder was spilled. It states the path and the *one* retrieval path that
// actually works here (see the package comment on why not grep).
//
// The caller passes it as the notice's recovery text, which means its bytes are
// reserved out of the cap like every other part of the notice — so spilling can
// never make a result larger than it would have been without it. That is the
// property that makes this safe to turn on everywhere: the worst case is a few
// hundred bytes of body traded for a locator, never a bigger turn.
func spillLocator(rel string, dropped int, existing string) string {
	s := fmt.Sprintf("The full %d-byte output is stored at %s — read_file it (with offset/limit to page)", dropped, rel)
	if existing != "" {
		return existing + ". " + s
	}
	return s
}

// SpillHead truncates s to limit keeping the head, spilling the whole of s to a
// file first so the dropped tail stays reachable. Falls back to a plain
// TruncateHead when the spill cannot be written.
func SpillHead(ctx context.Context, root, kind, s string, limit int, recovery string) string {
	if len(s) <= limit {
		return s
	}
	if rel, ok := spillText(ctx, root, kind, s); ok {
		recovery = spillLocator(rel, len(s), recovery)
	}
	out, _ := TruncateHead(s, limit, recovery)
	return out
}

// SpillTail is SpillHead's mirror for results whose information is at the end.
func SpillTail(ctx context.Context, root, kind, s string, limit int, recovery string) string {
	if len(s) <= limit {
		return s
	}
	if rel, ok := spillText(ctx, root, kind, s); ok {
		recovery = spillLocator(rel, len(s), recovery)
	}
	out, _ := TruncateTail(s, limit, recovery)
	return out
}

// spillItems is the item-level arm (P64.1's "half a careless port would miss"):
// glob and grep cap at the *result* level, so by the time any byte-oriented
// policy sees the result the tail matches are already gone. Only the tool that
// collected them can spill them, and only if it collected past its own inline
// cap in the first place — see grepSpillMaxMatches in search.go.
//
// items is the full collected set; max is the inline cap. capped says whether
// more exist beyond what was collected, which is a different question from
// whether items exceeded max (see capItems).
func spillItems(ctx context.Context, root, kind string, items []string, max int, capped bool) string {
	if !capped && len(items) <= max {
		return strings.Join(items, "\n")
	}
	inline := capItems(items, max, kind, true)
	if len(items) <= max {
		// Capped by the collector, not by the inline cap — there is nothing
		// extra on hand to spill, so the notice stands alone as before.
		return inline
	}
	rel, ok := spillText(ctx, root, kind, strings.Join(items, "\n"))
	if !ok {
		return inline
	}
	return inline + fmt.Sprintf("\n[%d of %d collected results shown. The full collected set is stored at %s — read_file it (with offset/limit to page).]",
		max, len(items), rel)
}

// reapSpills bounds the directory. Best-effort throughout: a file that will not
// stat or unlink (Windows, another process holding it open) is skipped rather
// than escalated, since failing to reap is a disk problem and failing the
// caller is a correctness problem.
func reapSpills(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type spillFile struct {
		path string
		mod  time.Time
		size int64
	}
	var files []spillFile
	var total int64
	cutoff := time.Now().Add(-spillTTL)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		files = append(files, spillFile{filepath.Join(dir, e.Name()), info.ModTime(), info.Size()})
		total += info.Size()
	}
	if len(files) <= spillMaxFiles && total <= spillMaxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for i := 0; i < len(files) && (len(files)-i > spillMaxFiles || total > spillMaxBytes); i++ {
		if os.Remove(files[i].path) == nil {
			total -= files[i].size
		}
	}
}
