package dialog

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func sampleGroups() []ProviderModels {
	return []ProviderModels{
		{Provider: "copilot", Models: []models.Model{{ID: "copilot.gpt-4o", Name: "GPT-4o", Provider: "copilot"}}},
		{Provider: "openai", Models: []models.Model{{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai"}}},
	}
}

func TestFormatProviderModelsGroupsAndMarksCurrent(t *testing.T) {
	out := FormatProviderModels(sampleGroups(), "gpt-4o")
	for _, want := range []string{"copilot\n", "openai\n", "  copilot.gpt-4o", "* gpt-4o"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(FormatProviderModels(nil, ""), "No models") {
		t.Fatal("empty list message")
	}
}

func TestResolveModelArg(t *testing.T) {
	g := sampleGroups()
	cases := map[string]models.ModelID{
		"gpt-4o":         "gpt-4o",
		"copilot.gpt-4o": "copilot.gpt-4o",
		"copilot/gpt-4o": "copilot.gpt-4o",
		"openai/gpt-4o":  "gpt-4o",
		"COPILOT/GPT-4O": "copilot.gpt-4o",
	}
	for in, want := range cases {
		m, err := ResolveModelArg(g, in)
		if err != nil || m.ID != want {
			t.Fatalf("%q -> %v, %v; want %s", in, m.ID, err, want)
		}
	}
	if _, err := ResolveModelArg(g, "nope"); err == nil {
		t.Fatal("unknown model must error")
	}
}
