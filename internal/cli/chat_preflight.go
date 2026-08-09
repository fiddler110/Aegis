package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/toolcallprobe"
)

// preflightFillTimeout bounds the probe. A phased drive that is about to spend
// tens of minutes can afford well under a minute to learn it should not start.
const preflightFillTimeout = 90 * time.Second

// preflightFillCheck refuses to start a phased drive on a model that cannot
// reliably fill a scaffolded document, and explains what to do instead.
//
// The gate exists because the two probes disagree in exactly the case that
// matters, and the reassuring one is the one an operator reads first. Measured
// (P38.1 re-test, 2026-08-09): LFM2.5-2.6B scored 5/5, 100%, on plain
// tool-calling and failed the structured multi-turn fill probe reproducibly.
// The drive then ran for two full attempts and produced zero output files.
// Emitting a tool call and sustaining a multi-turn targeted-edit loop are
// different competencies, and only the second predicts whether a drive
// finishes — so it is the second that should decide whether one starts.
//
// Deliberately a refusal rather than a warning. `aegis doctor --deep` already
// reports this as a WARN, and that is the right severity for a diagnostic
// command whose job is to describe. Here the cost of continuing is measured in
// tens of minutes of unattended tokens for a suite nobody will be able to use,
// so the default flips and --skip-model-check carries the override.
//
// Every inconclusive outcome allows the drive. A probe that could not run (an
// unreachable server, a cloud provider it does not cover, a model not resolved)
// says nothing about the model, and a gate that blocks on "unknown" would make
// the harness less usable than no gate at all.
func preflightFillCheck(ctx context.Context, cfg *config.Config, adapter provider.Adapter, errOut io.Writer, skip bool) error {
	if skip {
		fmt.Fprintf(errOut, "\n[notice: skipping the pre-flight structured-fill check (--skip-model-check)]\n")
		return nil
	}
	// Scoped to local Ollama-style providers, matching doctorDeepFillCheck:
	// the probe's failure shapes are calibrated against local models, and a
	// cloud model has already cleared this bar by construction.
	if ollamaNativeBase(cfg) == "" || cfg.Provider.Model == "" || cfg.Provider.Model == "auto" {
		return nil
	}
	if adapter == nil {
		return nil
	}

	fmt.Fprintf(errOut, "\n[notice: pre-flight — checking %q can sustain a multi-turn scaffolded fill before starting the drive]\n", cfg.Provider.Model)
	rctx, cancel := context.WithTimeout(ctx, preflightFillTimeout)
	defer cancel()

	res, err := toolcallprobe.RunDeepFill(rctx, adapter, cfg.Provider.Model)
	if err != nil {
		// A probe error never refuses, including an engine abort from the
		// tool-failure or loop breaker. It is tempting to treat those as the
		// verdict — the model did just spend six rounds failing the same edit —
		// and an earlier version of this check did exactly that. Live evidence
		// says otherwise: qwen3:14b aborts this probe with `edit_fill keeps
		// failing with "old_string occurs 3 times"` and then completes drive
		// phases cleanly, because the drive fills through fill_marker while the
		// probe still exercises the exact-match edit_fill path fill_marker was
		// built to replace (P39.14). Refusing on that abort would block a model
		// that demonstrably does the work.
		//
		// The probe should be re-cut against fill_marker; until it is, an abort
		// here measures a path the drive no longer takes, and the honest
		// reading is "inconclusive". A genuinely unfit model still fails with a
		// *result* — LFM2.5-2.6B returns ClobberedMarkers — which is checked
		// below and does refuse.
		fmt.Fprintf(errOut, "[notice: pre-flight check could not run (%v) — starting the drive anyway]\n", err)
		return nil
	}
	if res.Clean() {
		fmt.Fprintf(errOut, "[notice: pre-flight passed — starting the drive]\n")
		return nil
	}

	var shapes []string
	if res.FabricatedCompletion {
		shapes = append(shapes, "claimed completion without doing the work")
	}
	if res.ClobberedMarkers {
		shapes = append(shapes, "overwrote whole sections instead of filling one marker")
	}
	if res.TimedOut {
		shapes = append(shapes, "did not converge within the turn budget")
	}
	if len(shapes) == 0 {
		shapes = append(shapes, "failed the synthetic multi-section fill task")
	}

	return weakModelError(cfg.Provider.Model, shapes)
}

// weakModelError renders the refusal, shared by the failed-result and
// aborted-probe paths so both say the same thing.
func weakModelError(model string, shapes []string) error {
	return fmt.Errorf(`model %q is not able to drive a phased skill build.

Pre-flight structured-fill probe failed: %s.

This is the probe that predicts whether a drive finishes. A model can score
100%% on plain tool-calling and still fail here — emitting a tool call and
sustaining a multi-turn targeted-edit loop are different things, and a drive
started in this state typically burns its whole turn budget and writes nothing.

What to do:
  - use a larger local model for the drive (a 14B-class tool-calling model is
    the realistic floor; check 'ollama list')
  - or set provider.model per run: AEGIS_PROVIDER_MODEL=<model> aegis chat ...
  - or run 'aegis doctor --deep' for the full report
  - or pass --skip-model-check to start anyway`, model, joinShapes(shapes))
}

func joinShapes(shapes []string) string {
	switch len(shapes) {
	case 1:
		return shapes[0]
	case 2:
		return shapes[0] + " and " + shapes[1]
	default:
		out := ""
		for i, s := range shapes[:len(shapes)-1] {
			if i > 0 {
				out += ", "
			}
			out += s
		}
		return out + ", and " + shapes[len(shapes)-1]
	}
}
