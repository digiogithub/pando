package systemone

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRemoteLive runs against a hosted Jev. Opt-in via TYPESAFE_API_KEY (and
// optionally PANDO_LIVE_JEV_MODEL) or PANDO_LIVE_JEV_BASEURL + PANDO_LIVE_JEV_KEY.
func TestRemoteLive(t *testing.T) {
	kind, base, key := KindTypeSafe, "", os.Getenv("TYPESAFE_API_KEY")
	if b := os.Getenv("PANDO_LIVE_JEV_BASEURL"); b != "" {
		kind, base, key = KindCustom, b, os.Getenv("PANDO_LIVE_JEV_KEY")
	}
	if key == "" && base == "" {
		t.Skip("set TYPESAFE_API_KEY or PANDO_LIVE_JEV_BASEURL/PANDO_LIVE_JEV_KEY to run")
	}
	p, err := NewProvider(kind, Options{BaseURL: base, APIKey: key, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ms, _, err := p.ListDecisionModels(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	model := os.Getenv("PANDO_LIVE_JEV_MODEL")
	if model == "" {
		if len(ms) == 0 {
			t.Fatal("no models listed")
		}
		model = ms[0].ID
	}
	if len(ms) == 0 {
		t.Fatal("no models listed")
	}
	resp, err := p.Client().Decide(ctx, probeRequest(model, ""))
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("model=%s answer=%+v usage=%+v", model, resp.Answers["ok"], resp.Usage)
}
