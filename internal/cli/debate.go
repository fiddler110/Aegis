package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

// debateCLIResult is the machine-readable summary emitted in json mode,
// mirroring chatResult's convention (P4.5).
type debateCLIResult struct {
	Report     string  `json:"report"`
	Verdict    string  `json:"verdict"`
	Confidence string  `json:"confidence"`
	CostUSD    float64 `json:"cost_usd"`
	Turns      int     `json:"turns"`
	Error      string  `json:"error,omitempty"`
}

func newDebateCmd() *cobra.Command {
	var (
		domain          string
		files           []string
		proposerPersona string
		criticPersona   string
		arbiterPersona  string
		maxRounds       int
		outputFormat    string
	)

	cmd := &cobra.Command{
		Use:   "debate <claim>",
		Short: "Adversarially debate any claim (propose/critique/rebut/arbitrate)",
		Long: "Runs a multi-agent debate (P12) over the given claim — a security finding, threat/mitigation " +
			"pair, design assertion, or a claim about any document/plan/decision — instead of trusting a " +
			"single unchallenged pass: a critic (grounded in cited evidence from grep/read_file/web_fetch, " +
			"or an explicit concession) challenges the claim, the proposer rebuts, this repeats for " +
			"--max-rounds (default 2), then an arbiter issues a final UPHOLD/REVISE/REJECT verdict with a " +
			"confidence label. --domain generic swaps the default security-researcher/security-critic/" +
			"security-arbiter personas for general/critic/arbiter, for debating non-security claims; --file " +
			"points the roles at specific documents to ground the debate in instead of relying on recall. " +
			"Runs headless, no daemon required — same one-shot construction as `aegis chat`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			claim := strings.TrimSpace(strings.Join(args, " "))

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			stopOllama, err := ensureOllamaRunning(cfg)
			if err != nil {
				return err
			}
			defer stopOllama()
			if err := resolveOllamaModel(cfg); err != nil {
				return err
			}

			adapter, err := providerfactory.Build(cfg, nil, providerfactory.WithModelCaps(cfg.OpenModelCaps()))
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			reg := tool.NewRegistry()
			// P62.10: same as the worker — a non-interactive command that loads
			// cfg and must then honour what it says about the model. A debate is
			// several full engine runs per round (propose/critique/rebut/
			// arbitrate), so the per-turn schema cost is paid more times here than
			// anywhere else in the CLI. The security domain's roles reach for
			// security_scan, which the local profile defers rather than drops, so
			// a role that needs it loads it by name.
			// LocalProfile and the rest of the config-derived option set are decided
			// by enginecfg.BuiltinOptions (P62.10/P66.13) rather than re-listed here.
			if err := builtin.Register(reg, enginecfg.BuiltinOptions(cfg, cwd)); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			tracker := cost.NewTracker()
			// P66.13: a fourth bare gate, not in the review's four instances —
			// found by extracting the constructor. `aegis debate` built
			// permission.New(ModeBuild, AutoDeny{}) directly, so an operator's
			// deny rules and egress-then-write policy were inert for every
			// debate role. A role has no persona of its own, and AutoDeny is
			// kept deliberately: a debate role is an analysis run that should
			// never sit waiting for an approval nobody is watching for.
			gate, engineHooks := enginecfg.BuildGate(enginecfg.GateOptions{
				// A debate role runs in build mode, where StrictPlanMode is
				// inert by definition — passed anyway so a future mode change
				// here does not silently drop the operator's posture.
				Mode:           string(permission.ModeBuild),
				StrictPlanMode: !cfg.Permission.PlanModeShellReadsEnabled(),
				Approver:       permission.AutoDeny{},
				Security:       cfg.Security,
				Registry:       reg,
				Rules:          enginecfg.ConfigRules(cfg, nil),
			})
			debateCfg := debate.WithDefaults(debate.Config{
				Domain:          domain,
				ProposerPersona: proposerPersona,
				CriticPersona:   criticPersona,
				ArbiterPersona:  arbiterPersona,
				MaxRounds:       maxRounds,
				Tracker:         tracker,
				BudgetUSD:       cfg.Cost.BudgetUSD,
				MaxTokens:       cfg.Cost.MaxTokensPerRun,
			})
			// P69.6: plan all three seats against one memory budget before the
			// first of them runs, and refuse here rather than let Ollama discover
			// the overcommit by spilling to system RAM. nil when
			// provider.vram_budget_gb is unset, which is every install that has
			// not opted in.
			seatWindows, err := debateResidentPlan(ctx, cmd.ErrOrStderr(), cfg, debateCfg)
			if err != nil {
				return err
			}

			runRole := func(roleCtx context.Context, seat debate.Seat, systemPrompt, prompt string) (string, error) {
				// Per-seat model (P69.1), resolved through the same precedence
				// the daemon uses. Headless, so there is no context-window cache
				// to read a detected window out of the way the daemon's runner
				// has — but a planned window is knowable without one, and it
				// rides the adapter as a num_ctx stamp through exactly the
				// decorator modelAdapter uses. With no plan the window is
				// whatever the seat's own Modelfile pins, which is the reason
				// docs/local-model-tuning.md insists on pinning it per variant.
				model := enginecfg.DebateSeatModel(cfg, seat.Persona)
				seatAdapter := adapter
				if win := seatWindows[model]; win > 0 {
					seatAdapter = provider.WithNumCtx(adapter, win)
				}
				opts := engine.Options{
					Adapter: seatAdapter,
					Tools:   reg,
					Gate:    gate,
					Hooks:   engineHooks,
					Cost:    tracker,
					// P81.1: same AutoDeny{} as the gate above, and for the same
					// reason — a headless debate role must never sit waiting for
					// an approval nobody is watching for, so a scan hit is
					// withheld rather than blocked on.
					Approver:       permission.AutoDeny{},
					Purpose:        provider.PurposeDebate, // P67.3
					RoundResultCap: roundCapFor(cwd),       // P67.1
					Model:          model,
				}
				// P66.13/ARCH-06: one shared reading of config, so a seat gets
				// the operator's bounds and the same backend parameters the
				// session path uses. Inherited whole — a seat is a bounded
				// sub-run, and elapsed time is not divisible across seats.
				enginecfg.CostLimits(cfg).Apply(&opts)
				enginecfg.ModelBackend(cfg).Apply(&opts)
				eng, err := engine.New(opts)
				if err != nil {
					return "", err
				}
				conv := &engine.Conversation{System: systemPrompt}
				conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}})
				var sb strings.Builder
				runErr := eng.Run(roleCtx, conv, func(ev engine.Event) {
					if ev.Kind == engine.KindText {
						sb.WriteString(ev.Text)
					}
				})
				return strings.TrimSpace(sb.String()), runErr
			}

			claim = debate.WithFiles(claim, files)
			transcript, runErr := debate.Run(ctx, claim, debateCfg, runRole)

			snap := tracker.Snapshot()
			out := cmd.OutOrStdout()
			format, err := parseOutputFormat(outputFormat)
			if err != nil {
				return err
			}
			switch format {
			case outputJSON, outputStreamJSON:
				line, _ := json.Marshal(debateCLIResult{
					Report: transcript.Format(), Verdict: transcript.Verdict.Outcome,
					Confidence: transcript.Verdict.Confidence, CostUSD: snap.TotalUSD,
					Turns: snap.Turns, Error: errString(runErr),
				})
				fmt.Fprintln(out, string(line))
			default:
				fmt.Fprintln(out, transcript.Format())
				if snap.TotalUSD > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "\n[cost: $%.4f over %d turn(s)]\n", snap.TotalUSD, snap.Turns)
				}
			}
			return runErr
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "default persona trio: security (default) or generic, for non-security claims")
	cmd.Flags().StringArrayVar(&files, "file", nil, "file path the debate roles should read for grounding (repeatable)")
	cmd.Flags().StringVar(&proposerPersona, "proposer", "", "persona for the proposer role (default security-researcher, or general if --domain generic)")
	cmd.Flags().StringVar(&criticPersona, "critic", "", "persona for the critic role (default security-critic, or critic if --domain generic)")
	cmd.Flags().StringVar(&arbiterPersona, "arbiter", "", "persona for the arbiter role (default security-arbiter, or arbiter if --domain generic)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "maximum critique/rebuttal rounds before arbitration (default 2)")
	cmd.Flags().StringVar(&outputFormat, "output-format", "text", "output format: text or json (final transcript + verdict object)")
	return cmd
}
