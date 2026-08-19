package tool

import (
	"context"
	"testing"
)

func TestWithContextWindowRoundTrips(t *testing.T) {
	ctx := WithContextWindow(context.Background(), 16000)
	got, ok := ContextWindowFromContext(ctx)
	if !ok || got != 16000 {
		t.Fatalf("got (%d, %v), want (16000, true)", got, ok)
	}
}

func TestWithContextWindowZeroOrNegativeCarriesNothing(t *testing.T) {
	for _, n := range []int{0, -1} {
		ctx := WithContextWindow(context.Background(), n)
		if _, ok := ContextWindowFromContext(ctx); ok {
			t.Errorf("WithContextWindow(%d) should carry nothing, but ContextWindowFromContext reported ok", n)
		}
	}
}

func TestContextWindowFromContextUnset(t *testing.T) {
	if _, ok := ContextWindowFromContext(context.Background()); ok {
		t.Error("want ok=false against a bare context")
	}
}
