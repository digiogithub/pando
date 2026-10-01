package config

import (
	"strings"
	"testing"
)

func TestDecisionFilterDefaults(t *testing.T) {
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := Get().Remembrances
	if r.DecisionFilterThreshold() != 0.60 || r.DecisionFilterMaxCandidates() != 32 || r.DecisionFilterMaxCandidateChars() != 400 {
		t.Fatalf("defaults = %v %d %d", r.DecisionFilterThreshold(), r.DecisionFilterMaxCandidates(), r.DecisionFilterMaxCandidateChars())
	}
	if !r.DecisionFilterLocalOnly() || r.ContextEnrichmentDecisionFilterAllowHosted {
		t.Fatal("the filter must be local-only by default")
	}
	if r.ContextEnrichmentDecisionFilterEnabled || r.MemoryContextDecisionFilterEnabled {
		t.Fatal("the filter must be off by default")
	}
	if r.ContextEnrichmentDecisionFilterThreshold != 0.60 || r.ContextEnrichmentDecisionFilterMaxCandidates != 32 || r.ContextEnrichmentDecisionFilterMaxCandidateChars != 400 {
		t.Fatalf("loaded config must carry the defaults: %+v", r)
	}
	// Zero values (old clients, raw structs) fall back to the defaults.
	var zero RemembrancesConfig
	if zero.DecisionFilterThreshold() != 0.60 || zero.DecisionFilterMaxCandidates() != 32 || !zero.DecisionFilterLocalOnly() {
		t.Fatal("zero value must mean defaults and local-only")
	}
}

func TestValidateDecisionFilter(t *testing.T) {
	ok := []RemembrancesConfig{
		{},
		{ContextEnrichmentDecisionFilterThreshold: 1},
		{ContextEnrichmentDecisionFilterThreshold: 0.01, ContextEnrichmentDecisionFilterMaxCandidates: 1, ContextEnrichmentDecisionFilterMaxCandidateChars: 1},
	}
	for i, r := range ok {
		if err := r.ValidateDecisionFilter(); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	bad := map[string]RemembrancesConfig{
		"threshold":  {ContextEnrichmentDecisionFilterThreshold: 1.5},
		"negative t": {ContextEnrichmentDecisionFilterThreshold: -0.1},
		"max_cand":   {ContextEnrichmentDecisionFilterMaxCandidates: -1},
		"max_chars":  {ContextEnrichmentDecisionFilterMaxCandidateChars: -5},
	}
	for name, r := range bad {
		if err := r.ValidateDecisionFilter(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestUpdateRemembrancesRejectsInvalidDecisionFilter(t *testing.T) {
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	r := Get().Remembrances
	r.ContextEnrichmentDecisionFilterThreshold = 2
	err := UpdateRemembrances(r)
	if err == nil || !strings.Contains(err.Error(), "threshold") {
		t.Fatalf("err = %v", err)
	}
	if Get().Remembrances.ContextEnrichmentDecisionFilterThreshold == 2 {
		t.Fatal("an invalid update must not change the config")
	}
}

// The AllowHosted flag is stored inverted so an absent key means local-only: the
// config file is rewritten from a raw struct (no viper defaults) on every save,
// which would flip a "default true" bool to false.
func TestDecisionFilterPersistenceAndHotReloadEvent(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatal(err)
	}
	ch := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(ch)
	defer Bus.Unsubscribe(ch)

	r := Get().Remembrances
	r.ContextEnrichmentDecisionFilterEnabled = true
	r.MemoryContextDecisionFilterEnabled = true
	r.ContextEnrichmentDecisionFilterThreshold = 0.75
	r.ContextEnrichmentDecisionFilterMaxCandidates = 10
	r.ContextEnrichmentDecisionFilterMaxCandidateChars = 250
	r.ContextEnrichmentDecisionFilterAllowHosted = true
	if err := UpdateRemembrances(r); err != nil {
		t.Fatal(err)
	}
	if ev := <-ch; ev.Section != "remembrances" {
		t.Fatalf("event = %+v", ev)
	}

	if _, err := Load(dir, false); err != nil {
		t.Fatal(err)
	}
	got := Get().Remembrances
	if !got.ContextEnrichmentDecisionFilterEnabled || !got.MemoryContextDecisionFilterEnabled ||
		got.ContextEnrichmentDecisionFilterThreshold != 0.75 || got.ContextEnrichmentDecisionFilterMaxCandidates != 10 ||
		got.ContextEnrichmentDecisionFilterMaxCandidateChars != 250 || got.DecisionFilterLocalOnly() {
		t.Fatalf("round trip lost values: %+v", got)
	}

	// Back to the safe default: an explicit false is preserved, and an unrelated
	// save (which re-marshals the whole raw struct) does not flip it.
	got.ContextEnrichmentDecisionFilterAllowHosted = false
	if err := UpdateRemembrances(got); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTheme("dark"); err != nil {
		t.Logf("UpdateTheme unavailable: %v", err)
	}
	if _, err := Load(dir, false); err != nil {
		t.Fatal(err)
	}
	if !Get().Remembrances.DecisionFilterLocalOnly() {
		t.Fatal("local-only must survive save/load")
	}
}
