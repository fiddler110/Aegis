package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

// cmdDebate runs a multi-agent debate directly against the daemon's
// configured model and prints the formatted transcript — no session/prior
// conversational turn needed, same underlying mechanism as the `agent` tool's
// mode:"debate". Usage:
//
//	/debate <claim text>                     debate the given claim with the default (security) roles
//	/debate --domain generic <claim text>    use the general/critic/arbiter personas instead
//	/debate --file <path> [--file <path>...] <claim text>   ground the debate in specific documents
//	/debate --proposer/--critic/--arbiter <persona> <claim text>   override individual roles
//	/debate --max-rounds <n> <claim text>    override the critique/rebuttal round bound (default 2)
//
// Leading --flag value pairs are consumed before the remaining args are
// joined into the claim, mirroring the `aegis debate` CLI's flag names.
//
// Unlike /scan, a debate spends real model turns (one per role per round), so
// it uses the same long timeout /scan uses rather than the 5s default other
// direct-daemon-call commands use.
func (d *SlashDispatcher) cmdDebate(args []string) SlashResult {
	req, rest, err := parseDebateArgs(args)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	req.Claim = strings.TrimSpace(strings.Join(rest, " "))
	if req.Claim == "" {
		return SlashResult{Output: "usage: /debate [--domain generic] [--file <path>]... [--proposer|--critic|--arbiter <persona>] [--max-rounds <n>] <claim>", IsError: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := d.client.Debate(ctx, req)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Debate failed: %v", err), IsError: true}
	}
	return SlashResult{Output: resp.Report}
}

// parseDebateArgs consumes leading --flag value pairs recognized by /debate,
// returning the populated request (Claim left empty) and the remaining args
// to be joined as the claim text.
func parseDebateArgs(args []string) (api.DebateRequest, []string, error) {
	var req api.DebateRequest
	i := 0
	for i < len(args) {
		flag := args[i]
		if !strings.HasPrefix(flag, "--") {
			break
		}
		if i+1 >= len(args) {
			return req, nil, fmt.Errorf("flag %s requires a value", flag)
		}
		val := args[i+1]
		switch flag {
		case "--domain":
			req.Domain = val
		case "--file":
			req.Files = append(req.Files, val)
		case "--proposer":
			req.ProposerPersona = val
		case "--critic":
			req.CriticPersona = val
		case "--arbiter":
			req.ArbiterPersona = val
		case "--max-rounds":
			n, err := strconv.Atoi(val)
			if err != nil {
				return req, nil, fmt.Errorf("--max-rounds: %v", err)
			}
			req.MaxRounds = n
		default:
			// Not a recognized flag — treat it and everything after as claim text.
			return req, args[i:], nil
		}
		i += 2
	}
	return req, args[i:], nil
}

// threatModelFrameworkAliases maps a lowercase leading /threat-model token to
// its canonical framework name. "nist" alone is accepted since "800-154" is
// awkward to type and unambiguous in this context; if it's followed by a
// literal "800-154" token that token is consumed too.
var threatModelFrameworkAliases = map[string]string{
	"stride":       "STRIDE",
	"linddun":      "LINDDUN",
	"pasta":        "PASTA",
	"trike":        "Trike",
	"vast":         "VAST",
	"nist":         "NIST 800-154",
	"nist800-154":  "NIST 800-154",
	"nist-800-154": "NIST 800-154",
	"nist800154":   "NIST 800-154",
}

// extractThreatModelFramework consumes a recognized framework name from the
// front of /threat-model's args (e.g. "PASTA the auth service" ->
// ("PASTA", ["the", "auth", "service"])), so the framework can be given
// up front instead of only via the skill's clarifying question.
func extractThreatModelFramework(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	name, ok := threatModelFrameworkAliases[strings.ToLower(args[0])]
	if !ok {
		return "", args
	}
	rest := args[1:]
	if strings.EqualFold(args[0], "nist") && len(rest) > 0 && rest[0] == "800-154" {
		rest = rest[1:]
	}
	return name, rest
}

// cmdThreatModel sends a message that directly invokes the threat-modeling
// skill, so its framework-selection clarifying question is asked as part of
// the resulting turn rather than depending on the model noticing a trigger
// phrase in free text (P13.6 TUI-surface requirement). A leading framework
// name (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) is recognized and
// passed through as an explicit choice; without one, it opens a picker
// dialog instead of spending a model turn on the clarifying question.
func (d *SlashDispatcher) cmdThreatModel(args []string) SlashResult {
	// P52.12: an explicit unattended mode. Interactive stays the default —
	// P47.10's reasoning that review between phases is *valuable* still holds;
	// what was wrong was the absence of a choice, which forced anyone wanting
	// an unattended build out of the TUI entirely.
	args, unattended := extractUnattendedFlag(args)
	framework, rest := extractThreatModelFramework(args)
	target := strings.TrimSpace(strings.Join(rest, " "))
	if framework == "" {
		return SlashResult{ThreatModelTarget: &target, ThreatModelUnattended: unattended}
	}
	// The common case is "model the thing Aegis is actually running against"
	// — naming the workspace explicitly (and its path, when known) grounds
	// that instead of leaving the model to guess what "this project" means,
	// and matches the skill's own §2 instruction to explore the real
	// workspace rather than an assumed architecture.
	prompt := "Load the threat-modeling skill and produce a threat model for "
	if target != "" {
		prompt += target + ", scoped to the current workspace"
	} else {
		prompt += "the application and codebase in the current workspace"
	}
	if d.workDir != "" {
		prompt += fmt.Sprintf(" (%s)", d.workDir)
	}
	prompt += fmt.Sprintf(". Use the %s framework — this has already been decided, so skip the framework-selection clarifying question.", framework)
	if unattended {
		// The drive builds each phase's prompt itself from the skill's plan, so
		// it needs the task, not the skill body — no activateSkill here.
		return SlashResult{Drive: &api.DriveRequest{Skill: "threat-modeling", Task: prompt}}
	}
	body, warn := d.activateSkill("threat-modeling")
	return SlashResult{Output: warn, Message: skillTaskMessage("threat-modeling", body, prompt)}
}

// extractUnattendedFlag pulls an `unattended` / `--unattended` token from
// anywhere in args, returning the remaining args. Accepted anywhere rather than
// only in the leading position because it reads naturally at either end
// ("/threat-model unattended stride" and "/threat-model stride unattended"),
// and the framework/target parsing that follows must not see it either way.
func extractUnattendedFlag(args []string) (rest []string, unattended bool) {
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "unattended", "--unattended", "-u":
			unattended = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, unattended
}

// cmdDrive starts an unattended phased drive for any skill that declares a
// phase plan (P52.12) — the general form of /threat-model unattended, so a
// skill that opts in via its own `phases:` frontmatter is reachable from the
// TUI without a new slash command per skill.
func (d *SlashDispatcher) cmdDrive(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "usage: /drive <skill> <task…>", IsError: true}
	}
	skill := args[0]
	task := strings.TrimSpace(strings.Join(args[1:], " "))
	if task == "" {
		return SlashResult{Output: "usage: /drive <skill> <task…> — the task is required, it is what the drive builds", IsError: true}
	}
	return SlashResult{Drive: &api.DriveRequest{Skill: skill, Task: task}}
}
