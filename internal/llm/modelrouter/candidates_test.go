package modelrouter

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func lookupFrom(infos ...CandidateInfo) func(models.ModelID) (CandidateInfo, bool) {
	m := map[models.ModelID]CandidateInfo{}
	for _, i := range infos {
		m[i.ID] = i
	}
	return func(id models.ModelID) (CandidateInfo, bool) { i, ok := m[id]; return i, ok }
}

func TestCandidatesAttachments(t *testing.T) {
	lk := lookupFrom(
		CandidateInfo{ID: "A", Known: true, Enabled: true, SupportsAttachments: false, ContextWindow: 100000},
		CandidateInfo{ID: "B", Known: true, Enabled: true, SupportsAttachments: true, ContextWindow: 100000},
	)
	got, skipped := FilterCandidates([]models.ModelID{"A", "B", "A"}, lk, true, 0)
	if len(got) != 1 || got[0] != "B" || skipped["A"] == "" {
		t.Fatalf("got %v skipped %v", got, skipped)
	}
	got, _ = FilterCandidates([]models.ModelID{"A", "B"}, lk, false, 0)
	if len(got) != 2 {
		t.Fatalf("without attachments both usable, got %v", got)
	}
}

func TestCandidatesContextWindow(t *testing.T) {
	lk := lookupFrom(
		CandidateInfo{ID: "A", Known: true, Enabled: true, ContextWindow: 128000},
		CandidateInfo{ID: "B", Known: true, Enabled: true, ContextWindow: 200000},
	)
	got, skipped := FilterCandidates([]models.ModelID{"A", "B"}, lk, false, 150000)
	if len(got) != 1 || got[0] != "B" || skipped["A"] == "" {
		t.Fatalf("got %v skipped %v", got, skipped)
	}
}

func TestCandidatesUnusableRoute(t *testing.T) {
	lk := lookupFrom(
		CandidateInfo{ID: "A", Known: true, Enabled: false},
		CandidateInfo{ID: "B", Known: true, Enabled: false},
	)
	got, skipped := FilterCandidates([]models.ModelID{"A", "B", "ghost"}, lk, false, 0)
	if len(got) != 0 {
		t.Fatalf("expected empty chain, got %v", got)
	}
	if skipped["A"] != SkipDisabled || skipped["ghost"] != SkipUnknown {
		t.Fatalf("skipped %v", skipped)
	}
}

func TestDefaultLookup(t *testing.T) {
	var id models.ModelID
	var prov models.ModelProvider
	for k, m := range models.SupportedModels() {
		if m.AccountID == "" {
			id, prov = k, m.Provider
			break
		}
	}
	if id == "" {
		t.Skip("no static model in registry")
	}
	if _, ok := DefaultLookup(nil)(id); !ok {
		t.Fatal("known model not found")
	}
	if _, ok := DefaultLookup(nil)("nope/nothing"); ok {
		t.Fatal("unknown model reported known")
	}
	cfg := &config.Config{Providers: map[models.ModelProvider]config.Provider{prov: {Disabled: true}}}
	if info, _ := DefaultLookup(cfg)(id); info.Enabled {
		t.Fatal("disabled provider must not be enabled")
	}
}
