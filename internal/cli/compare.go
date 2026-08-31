package cli

import (
	"bufio"
	"context"
	crand "crypto/rand"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/spf13/cobra"
)

// P20.2 — blind model compare. Structurally this is `aegis parallel` with the
// varying axis flipped: parallel fans the *same* model out over *different*
// prompts; compare fans *different* models out over the *same* prompt. It's
// built as its own command rather than a parallel flag because the two have
// materially different output contracts — parallel just interleaves labeled
// progress, while compare must actively withhold model identity from
// everything printed before the vote, then reveal, then optionally
// synthesize — enough extra shape that bolting it onto parallel's flags would
// muddy both. runOneCompare deliberately mirrors runOneParallel's
// create-session/post-message/drain-events structure (including approval and
// error handling) so the two stay easy to compare side by side.

func newCompareCmd() *cobra.Command {
	var (
		mode        string
		autoApprove bool
		synthesize  bool
		synthModel  string
	)

	cmd := &cobra.Command{
		Use:   "compare <model-A> <model-B> [prompt]",
		Short: "Blind side-by-side comparison of two models on the same prompt",
		Long: "Runs the identical prompt against two models concurrently, each in its own daemon " +
			"session, and hides which response came from which model — shown only as \"Response 1\" / " +
			"\"Response 2\", in a randomized order so position isn't a tell — until you vote (1, 2, tie, " +
			"or skip). After voting, the reveal shows which model actually produced which response. Pass " +
			"--synthesize to make one further call (to --synth-model, or the configured default model) " +
			"asking it to combine the best of both revealed answers into a single, clearly labeled " +
			"synthesis — not a third blind response. The prompt can be given as trailing arguments or read " +
			"from stdin, matching `aegis chat`. Both sessions persist and can be resumed with " +
			"`aegis --resume <id>`, matching `aegis parallel`.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if mode == "" {
				mode = cfg.Permission.Mode
			}
			if mode == "" {
				mode = "build"
			}

			modelA, modelB := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if modelA == "" || modelB == "" {
				return fmt.Errorf("model-A and model-B must both be non-empty")
			}

			const maxPromptSize = 1 << 20 // 1 MiB, matching `aegis chat`
			prompt := strings.TrimSpace(strings.Join(args[2:], " "))
			if prompt == "" {
				data, _ := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPromptSize))
				prompt = strings.TrimSpace(string(data))
			}
			if prompt == "" {
				return fmt.Errorf("no prompt provided (pass as trailing arguments or via stdin)")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			cl, stopDaemon, err := ensureDaemon(ctx, cfg)
			if err != nil {
				return err
			}
			defer stopDaemon()

			out := cmd.OutOrStdout()
			var stdoutMu sync.Mutex
			logf := func(format string, a ...any) {
				stdoutMu.Lock()
				fmt.Fprintf(out, format, a...)
				stdoutMu.Unlock()
			}

			// Identities hidden until vote: randomize which model lands in
			// which slot so the fixed "Response 1"/"Response 2" labels aren't
			// themselves a positional tell across repeated runs.
			slot1Model, slot2Model := modelA, modelB
			if randomSwap() {
				slot1Model, slot2Model = modelB, modelA
			}

			var res1, res2 compareResult
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				res1 = runOneCompare(ctx, cl, "Response 1", slot1Model, mode, prompt, autoApprove, logf)
			}()
			go func() {
				defer wg.Done()
				res2 = runOneCompare(ctx, cl, "Response 2", slot2Model, mode, prompt, autoApprove, logf)
			}()
			wg.Wait()

			fmt.Fprintln(out)
			printCompareAnswer(out, res1)
			fmt.Fprintln(out)
			printCompareAnswer(out, res2)

			fmt.Fprint(out, "\nVote — which response is better? [1/2/tie/skip]: ")
			reader := bufio.NewReader(cmd.InOrStdin())
			line, _ := reader.ReadString('\n')
			vote, voteErr := parseCompareVote(line)
			if voteErr != nil {
				fmt.Fprintf(out, "%v — treating as skip.\n", voteErr)
			}

			fmt.Fprintln(out, "\nReveal:")
			fmt.Fprintf(out, "  Response 1 = %s\n", slot1Model)
			fmt.Fprintf(out, "  Response 2 = %s\n", slot2Model)
			fmt.Fprintln(out, describeCompareVote(vote, slot1Model, slot2Model))

			if synthesize {
				if err := runCompareSynthesis(ctx, cl, cfg, out, prompt, slot1Model, slot2Model, res1.answer, res2.answer, synthModel, mode); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "synthesis failed: %v\n", err)
				}
			}

			fmt.Fprintln(out, "\nSessions:")
			for _, r := range []compareResult{res1, res2} {
				status := "ok"
				if r.err != nil {
					status = "error: " + r.err.Error()
				}
				id := r.sessionID
				if id == "" {
					id = "(no session)"
				}
				fmt.Fprintf(out, "  %-11s %-12s %s (%d tool calls)\n", r.label, status, id, r.tools)
				if r.sessionID != "" {
					fmt.Fprintf(out, "              resume: aegis --resume %s\n", r.sessionID)
				}
			}

			if res1.err != nil && res2.err != nil {
				return fmt.Errorf("both runs failed: %v / %v", res1.err, res2.err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode for both sessions: plan, build (default), or auto")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "auto-approve tool calls that require confirmation (else they are denied)")
	cmd.Flags().BoolVar(&synthesize, "synthesize", false, "after voting, ask a model to synthesize the best of both revealed responses (default off)")
	cmd.Flags().StringVar(&synthModel, "synth-model", "", "model to use for --synthesize; default is the configured provider.model (a third model, not either compared one)")
	return cmd
}

// compareResult is the outcome of one blind-compare arm. model is carried for
// the caller's own bookkeeping (the reveal step, the synthesis prompt) — it
// must never be passed to logf, which is what keeps identity hidden during
// the run.
type compareResult struct {
	label     string // "Response 1" / "Response 2" — the only thing logged mid-run
	model     string
	sessionID string
	answer    string
	tools     int
	err       error
}

// runOneCompare creates a session pinned to model, posts the prompt, and
// drains its event stream — mirroring runOneParallel's shape (approval
// handling, error surfacing, tool-call counting) with two differences: it
// PATCHes the session to a specific model right after creating it (the P14.7
// mechanism: api.UpdateSessionRequest.Model via PATCH /sessions/{id}), and it
// accumulates the answer text instead of streaming it straight to the
// terminal, since the caller needs both full answers in hand before printing
// them side by side. Every logf call here is keyed by label, never model —
// that's the actual mechanism that keeps identity hidden until the reveal.
func runOneCompare(ctx context.Context, cl *client.Client, label, model, mode, prompt string, autoApprove bool, logf func(string, ...any)) (res compareResult) {
	res.label = label
	res.model = model

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: mode, Origin: reqorigin.CLI})
	if err != nil {
		res.err = err
		return
	}
	res.sessionID = meta.ID

	if _, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Model: &model}); err != nil {
		res.err = err
		return
	}
	logf("[%s] started\n", label)

	events, err := cl.PostMessage(ctx, meta.ID, prompt)
	if err != nil {
		res.err = err
		return
	}
	var sb strings.Builder
	for ev := range events {
		switch ev.Kind {
		case api.KindText:
			sb.WriteString(ev.Text)
		case api.KindToolCall:
			res.tools++
			logf("[%s] \U0001F527 %s\n", label, ev.Tool)
		case api.KindApprovalRequest:
			actx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = cl.SendApproval(actx, meta.ID, ev.ApprovalID, autoApprove, false)
			cancel()
			if autoApprove {
				logf("[%s] ✓ approved %s\n", label, ev.Tool)
			} else {
				logf("[%s] ✗ denied %s (use --yes to allow)\n", label, ev.Tool)
			}
		case api.KindError:
			res.err = fmt.Errorf("%s", ev.Error)
			logf("[%s] error: %s\n", label, ev.Error)
		}
	}
	res.answer = strings.TrimSpace(sb.String())
	logf("[%s] done (%d tool calls)\n", label, res.tools)
	return
}

func printCompareAnswer(w io.Writer, r compareResult) {
	fmt.Fprintf(w, "=== %s ===\n", r.label)
	if r.err != nil {
		fmt.Fprintf(w, "(error: %v)\n", r.err)
		return
	}
	if r.answer == "" {
		fmt.Fprintln(w, "(no output)")
		return
	}
	fmt.Fprintln(w, r.answer)
}

// compareVote is the parsed result of the post-response vote prompt.
type compareVote int

const (
	voteSkip compareVote = iota
	voteOne
	voteTwo
	voteTie
)

// parseCompareVote parses the user's answer to the "1/2/tie/skip" prompt.
// Blank input (just pressing enter) is treated as skip, same as an explicit
// "skip"; anything else unrecognized is also treated as skip by the caller,
// but returned as an error so the caller can say so.
func parseCompareVote(raw string) (compareVote, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "1":
		return voteOne, nil
	case "2":
		return voteTwo, nil
	case "tie":
		return voteTie, nil
	case "", "skip":
		return voteSkip, nil
	default:
		return voteSkip, fmt.Errorf("invalid vote %q (want 1, 2, tie, or skip)", raw)
	}
}

// describeCompareVote renders the post-reveal vote line. It is only ever
// called after slot1Model/slot2Model have already been printed by the
// reveal, so naming the model here does not reintroduce an early leak.
func describeCompareVote(v compareVote, slot1Model, slot2Model string) string {
	switch v {
	case voteOne:
		return fmt.Sprintf("Vote: Response 1 (%s)", slot1Model)
	case voteTwo:
		return fmt.Sprintf("Vote: Response 2 (%s)", slot2Model)
	case voteTie:
		return "Vote: tie"
	default:
		return "Vote: skipped"
	}
}

// randomSwap decides, per run, whether to swap model-A/model-B into
// slot2/slot1 instead of slot1/slot2 — so which slot a given model lands in
// varies across runs instead of always being the order the user typed them
// in. Falls back to "no swap" on the (essentially never, on any real OS) case
// crypto/rand can't be read, which just means position randomization is
// skipped for that one run — identity is still hidden until vote either way.
func randomSwap() bool {
	var b [1]byte
	if _, err := crand.Read(b[:]); err != nil {
		return false
	}
	return b[0]&1 == 1
}

// runCompareSynthesis makes one further call — to synthModel if given, else
// cfg.Provider.Model (a third configured model, distinct from either
// compared model, per P20.2) — asking it to combine the two now-revealed
// answers into a single synthesized answer. Runs in its own persisted
// session, same as the two compared arms.
func runCompareSynthesis(ctx context.Context, cl *client.Client, cfg *config.Config, out io.Writer, prompt, modelA, modelB, answerA, answerB, synthModel, mode string) error {
	if synthModel == "" {
		synthModel = cfg.Provider.Model
	}
	if synthModel == "" {
		return fmt.Errorf("no --synth-model given and no default provider.model configured")
	}

	synthPrompt := fmt.Sprintf(
		"Two models answered the same prompt below. Synthesize the single best answer, combining "+
			"their strengths and resolving any disagreement between them. Do not just pick one answer "+
			"verbatim — produce one answer that is better than either alone.\n\n"+
			"Original prompt:\n%s\n\n--- %s's answer ---\n%s\n\n--- %s's answer ---\n%s\n",
		prompt, modelA, answerA, modelB, answerB,
	)

	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: mode, Origin: reqorigin.CLI})
	if err != nil {
		return err
	}
	if _, err := cl.UpdateSession(ctx, meta.ID, api.UpdateSessionRequest{Model: &synthModel}); err != nil {
		return err
	}
	events, err := cl.PostMessage(ctx, meta.ID, synthPrompt)
	if err != nil {
		return err
	}
	var sb strings.Builder
	for ev := range events {
		switch ev.Kind {
		case api.KindText:
			sb.WriteString(ev.Text)
		case api.KindError:
			return fmt.Errorf("%s", ev.Error)
		}
	}

	fmt.Fprintf(out, "\n=== Synthesis (%s) ===\n", synthModel)
	fmt.Fprintln(out, strings.TrimSpace(sb.String()))
	fmt.Fprintf(out, "resume: aegis --resume %s\n", meta.ID)
	return nil
}
