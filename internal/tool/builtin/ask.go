package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scottymacleod/agentharness/internal/tool"
)

// Questioner resolves structured questions to the user. In a TUI context this
// renders a multi-choice prompt; in non-interactive contexts it returns a
// default or error.
type Questioner interface {
	Ask(ctx context.Context, q Question) (string, error)
}

// Question is a structured question the model wants to ask the user.
type Question struct {
	Text    string   `json:"text"`
	Options []string `json:"options"` // optional; free-form if empty
}

// AutoAnswer always returns a fixed answer (for testing / non-interactive).
type AutoAnswer struct{ Answer string }

func (a AutoAnswer) Ask(context.Context, Question) (string, error) {
	return a.Answer, nil
}

type askTool struct {
	questioner Questioner
}

func (t *askTool) Name() string                { return "ask_user" }
func (t *askTool) Capability() tool.Capability { return tool.CapRead }
func (t *askTool) Description() string {
	return "Ask the user a question and wait for their answer. " +
		"Use this when you need clarification or a decision from the user. " +
		"Optionally provide choices for a structured multi-choice question."
}
func (t *askTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"question":{"type":"string","description":"the question to ask"},"options":{"type":"array","items":{"type":"string"},"description":"optional list of choices"}},"required":["question"]}`)
}
func (t *askTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Question) == "" {
		return tool.Result{Content: "question is required", IsError: true}, nil
	}

	q := Question{Text: args.Question, Options: args.Options}
	answer, err := t.questioner.Ask(ctx, q)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("ask_user failed: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: answer}, nil
}
