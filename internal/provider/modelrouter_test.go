package provider

import (
	"context"
	"testing"
)

type namedAdapter struct{ name string }

func (a namedAdapter) Name() string { return a.name }
func (a namedAdapter) Stream(context.Context, Request) (<-chan Event, error) {
	ch := make(chan Event, 1)
	ch <- Event{Type: EventTextDelta, Text: a.name}
	close(ch)
	return ch, nil
}

func servedBy(t *testing.T, a Adapter, model string) string {
	t.Helper()
	ch, err := a.Stream(context.Background(), Request{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	return (<-ch).Text
}

func TestModelRouterRoutesBySmallModel(t *testing.T) {
	r := WithModelRouter(namedAdapter{"main"}, "haiku", namedAdapter{"small"})
	if got := servedBy(t, r, "opus"); got != "main" {
		t.Errorf("main model served by %s", got)
	}
	if got := servedBy(t, r, "haiku"); got != "small" {
		t.Errorf("small model served by %s", got)
	}
	if u, ok := r.(interface{ Unwrap() Adapter }); !ok || u.Unwrap().Name() != "main" {
		t.Error("Unwrap must reach the primary so capability helpers answer for it")
	}
}

func TestModelRouterIsIdentityWithoutASmallBackend(t *testing.T) {
	main := namedAdapter{"main"}
	if WithModelRouter(main, "haiku", nil) != Adapter(main) || WithModelRouter(main, "", namedAdapter{"s"}) != Adapter(main) {
		t.Error("no small backend must leave the primary unwrapped")
	}
}
