package provider

import (
	"encoding/json"
	"fmt"
)

// Block JSON uses a "type" discriminator so messages round-trip through
// persistence and the daemon API without losing block identity.

type wireBlockJSON struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking
	Signature string `json:"signature,omitempty"`
	// image
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type wireMessageJSON struct {
	Role    Role            `json:"role"`
	Content []wireBlockJSON `json:"content"`
}

func encodeBlock(b Block) (wireBlockJSON, error) {
	switch v := b.(type) {
	case TextBlock:
		return wireBlockJSON{Type: "text", Text: v.Text}, nil
	case ToolUseBlock:
		return wireBlockJSON{Type: "tool_use", ID: v.ID, Name: v.Name, Input: v.Input}, nil
	case ToolResultBlock:
		return wireBlockJSON{Type: "tool_result", ToolUseID: v.ToolUseID, Content: v.Content, IsError: v.IsError}, nil
	case ThinkingBlock:
		return wireBlockJSON{Type: "thinking", Text: v.Text, Signature: v.Signature}, nil
	case ImageBlock:
		return wireBlockJSON{Type: "image", MediaType: v.MediaType, Data: v.Data}, nil
	default:
		return wireBlockJSON{}, fmt.Errorf("provider: cannot encode block %T", b)
	}
}

func decodeBlock(w wireBlockJSON) (Block, error) {
	switch w.Type {
	case "text":
		return TextBlock{Text: w.Text}, nil
	case "tool_use":
		return ToolUseBlock{ID: w.ID, Name: w.Name, Input: w.Input}, nil
	case "tool_result":
		return ToolResultBlock{ToolUseID: w.ToolUseID, Content: w.Content, IsError: w.IsError}, nil
	case "thinking":
		return ThinkingBlock{Text: w.Text, Signature: w.Signature}, nil
	case "image":
		return ImageBlock{MediaType: w.MediaType, Data: w.Data}, nil
	default:
		return nil, fmt.Errorf("provider: unknown block type %q", w.Type)
	}
}

// MarshalJSON encodes a Message with block type discriminators so it can be
// stored and sent over the wire and faithfully restored.
func (m Message) MarshalJSON() ([]byte, error) {
	wm := wireMessageJSON{Role: m.Role}
	for _, b := range m.Content {
		wb, err := encodeBlock(b)
		if err != nil {
			return nil, err
		}
		wm.Content = append(wm.Content, wb)
	}
	return json.Marshal(wm)
}

// UnmarshalJSON restores a Message, reconstructing concrete block types from
// their "type" discriminator.
func (m *Message) UnmarshalJSON(data []byte) error {
	var wm wireMessageJSON
	if err := json.Unmarshal(data, &wm); err != nil {
		return err
	}
	m.Role = wm.Role
	m.Content = nil
	for _, wb := range wm.Content {
		b, err := decodeBlock(wb)
		if err != nil {
			return err
		}
		m.Content = append(m.Content, b)
	}
	return nil
}

// MarshalMessages serializes messages to JSON with block type discriminators.
func MarshalMessages(msgs []Message) ([]byte, error) {
	if msgs == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(msgs)
}

// UnmarshalMessages restores messages produced by MarshalMessages.
func UnmarshalMessages(data []byte) ([]Message, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var out []Message
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
