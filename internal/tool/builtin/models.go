package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/discover"
	"github.com/scottymacleod/aegis/internal/tool"
)

// modelsTool lets the model discover locally available models.
type modelsTool struct{}

func (t *modelsTool) Name() string                { return "list_models" }
func (t *modelsTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *modelsTool) Description() string {
	return "Discover locally running model servers (Ollama, LM Studio, LiteLLM) " +
		"and list their available models."
}
func (t *modelsTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{}}`)
}
func (t *modelsTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	models := discover.Discover(ctx, discover.DefaultSources(), 3*time.Second)
	if len(models) == 0 {
		return tool.Result{Content: "no local model servers found (checked Ollama :11434, LM Studio :1234, LiteLLM :4000)"}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d model(s) found:\n", len(models))
	for _, m := range models {
		fmt.Fprintf(&sb, "  %-12s  %s  (%s)\n", m.Provider, m.Name, m.Endpoint)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}
