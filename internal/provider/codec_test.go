package provider

import "testing"

func TestThinkingBlockRoundTrip(t *testing.T) {
	in := []Message{{
		Role: RoleAssistant,
		Content: []Block{
			ThinkingBlock{Text: "reasoning here", Signature: "sig-abc"},
			TextBlock{Text: "answer"},
		},
	}}
	blob, err := MarshalMessages(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := UnmarshalMessages(blob)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tb, ok := out[0].Content[0].(ThinkingBlock)
	if !ok {
		t.Fatalf("first block = %T, want ThinkingBlock", out[0].Content[0])
	}
	if tb.Text != "reasoning here" || tb.Signature != "sig-abc" {
		t.Errorf("thinking round-trip = %+v", tb)
	}
}

func TestImageBlockRoundTrip(t *testing.T) {
	in := []Message{{
		Role: RoleUser,
		Content: []Block{
			TextBlock{Text: "what is this?"},
			ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
		},
	}}
	blob, err := MarshalMessages(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := UnmarshalMessages(blob)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ib, ok := out[0].Content[1].(ImageBlock)
	if !ok {
		t.Fatalf("second block = %T, want ImageBlock", out[0].Content[1])
	}
	if ib.MediaType != "image/png" || ib.Data != "aGVsbG8=" {
		t.Errorf("image round-trip = %+v", ib)
	}
}
