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
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/security"
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
			if err := builtin.Register(reg, builtin.Options{
				Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL,
				SecurityScan:       security.OptionsFromConfig(cfg.Security),
				DASTAllowedTargets: cfg.Security.DAST.AllowedTargets,
				DASTAllowActive:    cfg.Security.DAST.AllowActive,
				LocalProfile:       cfg.Provider.LocalPromptProfile(),
				ToolFamilies:       cfg.Tools.Families,
			}); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			tracker := cost.NewTracker()
			runRole := func(roleCtx context.Context, systemPrompt, prompt string) (string, error) {
				gate := permission.New(permission.ModeBuild, permission.AutoDeny{})
				eng, err := engine.New(engine.Options{
					Adapter:         adapter,
					Tools:           reg,
					Gate:            gate,
					Cost:            tracker,
					Purpose:         provider.PurposeDebate, // P67.3
					RoundResultCap:  roundCapFor(cwd),       // P67.1
					BudgetUSD:       cfg.Cost.BudgetUSD,
					MaxTokensPerRun: cfg.Cost.MaxTokensPerRun,
					Model:           cfg.Provider.Model,
					MaxTokens:       cfg.Provider.MaxTokens,
				})
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
			transcript, runErr := debate.Run(ctx, claim, debate.Config{
				Domain:          domain,
				ProposerPersona: proposerPersona,
				CriticPersona:   criticPersona,
				ArbiterPersona:  arbiterPersona,
				MaxRounds:       maxRounds,
				Tracker:         tracker,
				BudgetUSD:       cfg.Cost.BudgetUSD,
				MaxTokens:       cfg.Cost.MaxTokensPerRun,
			}, runRole)

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
