package core

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/session"
)

func TestStatusAutoLabel(t *testing.T) {
	prev := config.Get()
	t.Cleanup(func() { config.SetForTests(prev) })
	yes := true
	config.SetForTests(&config.Config{ModelAutoMode: config.ModelAutoModeConfig{Enabled: true, Selected: &yes}})

	if got := (statusCmp{}).autoLabel(); got != "Auto" {
		t.Errorf("no session: %q", got)
	}
	s := statusCmp{session: session.Session{ID: "s-auto-label"}}
	if got := s.autoLabel(); got != "Auto" {
		t.Errorf("before first route: %q", got)
	}
	config.SetForTests(&config.Config{})
	if got := s.autoLabel(); got != "" {
		t.Errorf("disabled: %q", got)
	}
}
