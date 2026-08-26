package tools

import (
	"context"
	"strings"
	"testing"
)

// The gallery tool reads bundles embedded in the binary, so it must answer
// without a project, a database or the design subsystem being wired: a user
// asking what can be built should get an answer wherever they ask.
func TestDesignSkillsListNeedsNoSubsystem(t *testing.T) {
	tool := NewDesignSkillsTool(nil)
	response, err := tool.Run(context.Background(), ToolCall{Input: `{"action":"list"}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if response.IsError {
		t.Fatalf("list failed: %s", response.Content)
	}
	for _, want := range []string{"landing-page", "deck-basic", "design_create"} {
		if !strings.Contains(response.Content, want) {
			t.Errorf("the listing never mentions %q", want)
		}
	}
	if !strings.Contains(response.Content, "example brief:") {
		t.Error("the listing offers no starter brief")
	}
}

// "show" is what the model reads before building, so it must return the skill
// body, and a craft reference when asked for one.
func TestDesignSkillsShow(t *testing.T) {
	tool := NewDesignSkillsTool(nil)

	response, err := tool.Run(context.Background(), ToolCall{Input: `{"action":"show","name":"landing-page"}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if response.IsError || !strings.Contains(response.Content, "od:") {
		t.Fatalf("show returned %q", truncateForTest(response.Content))
	}

	response, err = tool.Run(context.Background(), ToolCall{Input: `{"action":"show","craft":"anti-ai-slop"}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if response.IsError || !strings.Contains(response.Content, "Anti-slop") {
		t.Fatalf("craft reference returned %q", truncateForTest(response.Content))
	}
}

// A name nobody ships must come back as a clear error naming what does exist,
// not as an empty result the model then guesses around.
func TestDesignSkillsRejectsUnknownNames(t *testing.T) {
	tool := NewDesignSkillsTool(nil)

	response, _ := tool.Run(context.Background(), ToolCall{Input: `{"action":"show","name":"vibes"}`})
	if !response.IsError || !strings.Contains(response.Content, "landing-page") {
		t.Errorf("unknown template: %q", response.Content)
	}

	response, _ = tool.Run(context.Background(), ToolCall{Input: `{"action":"show","craft":"vibes"}`})
	if !response.IsError || !strings.Contains(response.Content, "typography") {
		t.Errorf("unknown craft reference: %q", response.Content)
	}

	response, _ = tool.Run(context.Background(), ToolCall{Input: `{"action":"install"}`})
	if !response.IsError {
		t.Error("install without a name must fail")
	}
}

func truncateForTest(s string) string {
	if len(s) <= 120 {
		return s
	}
	return s[:120] + "…"
}
