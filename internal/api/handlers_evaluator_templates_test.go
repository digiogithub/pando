package api

import (
	"testing"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/prompt"
)

func TestBuildTemplateSections(t *testing.T) {
	files := []prompt.VariantFile{
		{Section: "base/workflow", Name: "terse", ID: "base/workflow#terse", Path: "/p/terse.md.tpl"},
		{Section: "base/workflow", Name: "verbose", ID: "base/workflow#verbose", Path: "/p/verbose.md.tpl"},
	}
	stats := []db.PromptVariantStat{
		{VariantID: "base/workflow#default", Section: "base/workflow", TimesUsed: 3, AvgReward: 0.4},
		{VariantID: "base/workflow#terse", Section: "base/workflow", TimesUsed: 3, AvgReward: 0.9},
		{VariantID: "base/workflow#gone", Section: "base/workflow", TimesUsed: 1, AvgReward: 0.5},
	}
	sections := buildTemplateSections(files, stats, 1.41)
	if len(sections) != 1 || sections[0].Section != "base/workflow" {
		t.Fatalf("unexpected sections: %+v", sections)
	}
	v := sections[0].Variants
	if len(v) != 4 {
		t.Fatalf("want default + terse + verbose + gone, got %+v", v)
	}
	if !v[0].IsDefault || v[0].TimesUsed != 3 {
		t.Errorf("default first with stats, got %+v", v[0])
	}
	byName := map[string]TemplateVariantResponse{}
	for _, x := range v {
		byName[x.Name] = x
	}
	if byName["terse"].Path != "/p/terse.md.tpl" || byName["terse"].UCBScore <= byName["default"].UCBScore {
		t.Errorf("terse should rank above default: %+v vs %+v", byName["terse"], byName["default"])
	}
	if byName["verbose"].TimesUsed != 0 || byName["verbose"].UCBScore != 0 {
		t.Errorf("untried variant has no stats: %+v", byName["verbose"])
	}
	if !byName["gone"].Missing {
		t.Errorf("stats-only variant must be flagged missing: %+v", byName["gone"])
	}
}
