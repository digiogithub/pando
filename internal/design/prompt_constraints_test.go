package design

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// The constraint block is paid for on every request the coder makes, so when it
// appears is as important as what it says.
func TestPromptConstraintsCostNothingUntilAProjectCommitsASystem(t *testing.T) {
	project := t.TempDir()
	previous := config.Get()
	t.Cleanup(func() { config.SetForTests(previous) })

	cfg := &config.Config{WorkingDir: project}
	cfg.Design.OutputDir = "designer"
	cfg.Design.SystemDir = "_system"
	config.SetForTests(cfg)
	if got := PromptConstraints(); got != "" {
		t.Errorf("an uncommitted system must add nothing to the prompt, got %d bytes", len(got))
	}

	// Committed: the block appears and names the files the designer must use.
	svc := NewService(nil, nil, NewLayout(project, "designer", "_system"), "")
	ds := DefaultDesignSystem()
	ds.Name = "House"
	if _, _, err := svc.SaveSystem(ds); err != nil {
		t.Fatalf("save system: %v", err)
	}
	block := PromptConstraints()
	for _, want := range []string{"House", "designer/_system/system.css", "designer/_system/DESIGN.md", "--color-accent"} {
		if !strings.Contains(block, want) {
			t.Errorf("constraint block is missing %q:\n%s", want, block)
		}
	}
}
