package depth

import (
	"context"
	"testing"
)

func TestResolveOrder(t *testing.T) {
	t.Setenv(EnvVar, "2")
	ctx := context.Background()

	if got := Resolve(ctx, ""); got != 2 {
		t.Fatalf("env fallback = %d, want 2", got)
	}

	SetSession("warm-sess", 4)
	t.Cleanup(func() { ClearSession("warm-sess") })
	if got := Resolve(ctx, "warm-sess"); got != 4 {
		t.Fatalf("session record must win over env, got %d", got)
	}

	if got := Resolve(WithContext(ctx, 1), "warm-sess"); got != 1 {
		t.Fatalf("explicit context must win over session, got %d", got)
	}

	t.Setenv(EnvVar, "garbage")
	if got := Resolve(ctx, ""); got != 0 {
		t.Fatalf("invalid env must read as top level, got %d", got)
	}
}

func TestFromMeta(t *testing.T) {
	cases := []struct {
		meta map[string]any
		want int
		ok   bool
	}{
		{nil, 0, false},
		{map[string]any{}, 0, false},
		{map[string]any{MetaKey: float64(3)}, 3, true}, // JSON-decoded number
		{map[string]any{MetaKey: 2}, 2, true},
		{map[string]any{MetaKey: "1"}, 1, true},
		{map[string]any{MetaKey: float64(-1)}, 0, false},
		{map[string]any{MetaKey: "x"}, 0, false},
	}
	for _, c := range cases {
		got, ok := FromMeta(c.meta)
		if got != c.want || ok != c.ok {
			t.Errorf("FromMeta(%v) = (%d,%v), want (%d,%v)", c.meta, got, ok, c.want, c.ok)
		}
	}
	if got, ok := FromMeta(Meta(5)); !ok || got != 5 {
		t.Fatalf("Meta round trip = (%d,%v)", got, ok)
	}
}

func TestSessionRegistry(t *testing.T) {
	SetSession("", 3) // ignored
	if _, ok := Session(""); ok {
		t.Fatal("empty session id must not be recorded")
	}
	SetSession("s", 1)
	ClearSession("s")
	if _, ok := Session("s"); ok {
		t.Fatal("ClearSession did not forget the session")
	}
}
