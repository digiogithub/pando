package agui

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// TestConfigFromAppResolvesProfiles is the PANDO-US-0013 acceptance
// criterion for how ConfigFromApp absorbs [AGUI.Profiles.<name>]: a profile
// that declares none of its own Tools/Mesnada/Persona inherits the
// adapter-wide value, while an explicit profile value (including an
// explicit empty Tools list) overrides it.
func TestConfigFromAppResolvesProfiles(t *testing.T) {
	falsePtr := new(bool)
	emptyTools := []string{}
	ownTools := []string{"gintrack__*"}
	denyTools := []string{"bash"}

	src := config.AGUIConfig{
		Tools:   []string{"glob", "grep"},
		Mesnada: nil, // adapter-wide default: true
		Persona: "adapter-persona",
		Profiles: map[string]config.AGUIProfile{
			"inherits-nothing": {
				Base: config.AgentCoder,
			},
			"overrides-everything": {
				Base:      config.AgentTask,
				Model:     "claude-sonnet-4-20250514",
				Persona:   "own-persona",
				Prompt:    "Stay terse.",
				Tools:     &ownTools,
				DenyTools: &denyTools,
				Mesnada:   falsePtr,
			},
			"explicit-empty-tools": {
				Base:  config.AgentCoder,
				Tools: &emptyTools,
			},
		},
	}

	out := ConfigFromApp(src)

	if len(out.Profiles) != 3 {
		t.Fatalf("resolved %d profiles, want 3: %+v", len(out.Profiles), out.Profiles)
	}

	inherits, ok := out.Profiles["inherits-nothing"]
	if !ok {
		t.Fatal("inherits-nothing profile missing")
	}
	if inherits.Base != config.AgentCoder {
		t.Fatalf("inherits.Base = %q, want coder", inherits.Base)
	}
	if len(inherits.Tools) != 2 || inherits.Tools[0] != "glob" || inherits.Tools[1] != "grep" {
		t.Fatalf("inherits.Tools = %v, want the adapter-wide [glob grep] fallback", inherits.Tools)
	}
	if !inherits.Mesnada {
		t.Fatal("inherits.Mesnada = false, want the adapter-wide default true")
	}
	if inherits.Persona != "adapter-persona" {
		t.Fatalf("inherits.Persona = %q, want the adapter-wide fallback", inherits.Persona)
	}
	if inherits.Prompt != "" {
		t.Fatalf("inherits.Prompt = %q, want empty (no adapter-wide Prompt to fall back to)", inherits.Prompt)
	}

	overrides, ok := out.Profiles["overrides-everything"]
	if !ok {
		t.Fatal("overrides-everything profile missing")
	}
	if overrides.Base != config.AgentTask {
		t.Fatalf("overrides.Base = %q, want task", overrides.Base)
	}
	if string(overrides.Model) != "claude-sonnet-4-20250514" {
		t.Fatalf("overrides.Model = %q", overrides.Model)
	}
	if overrides.Persona != "own-persona" {
		t.Fatalf("overrides.Persona = %q, want its own value, not the adapter-wide fallback", overrides.Persona)
	}
	if overrides.Prompt != "Stay terse." {
		t.Fatalf("overrides.Prompt = %q", overrides.Prompt)
	}
	if len(overrides.Tools) != 1 || overrides.Tools[0] != "gintrack__*" {
		t.Fatalf("overrides.Tools = %v, want its own [gintrack__*], not the adapter-wide fallback", overrides.Tools)
	}
	if len(overrides.DenyTools) != 1 || overrides.DenyTools[0] != "bash" {
		t.Fatalf("overrides.DenyTools = %v", overrides.DenyTools)
	}
	if overrides.Mesnada {
		t.Fatal("overrides.Mesnada = true, want its own explicit false")
	}

	explicitEmpty, ok := out.Profiles["explicit-empty-tools"]
	if !ok {
		t.Fatal("explicit-empty-tools profile missing")
	}
	if explicitEmpty.Tools == nil || len(explicitEmpty.Tools) != 0 {
		t.Fatalf("explicitEmpty.Tools = %v, want a non-nil empty list (explicit override, NOT the adapter-wide [glob grep] fallback)", explicitEmpty.Tools)
	}
}

// TestConfigFromAppSkipsInvalidProfilesDefensively guards ConfigFromApp
// against being called (e.g. directly from a test) on a config that bypassed
// config.Validate: a profile with an unknown Base, or one colliding with a
// KnownAgentNames entry, must not surface in the resolved Config.
func TestConfigFromAppSkipsInvalidProfilesDefensively(t *testing.T) {
	src := config.AGUIConfig{
		Profiles: map[string]config.AGUIProfile{
			"unknown-base":            {Base: config.AgentName("ghost-agent")},
			string(config.AgentCoder): {Base: config.AgentTask},
			"fine":                    {Base: config.AgentCoder},
		},
	}
	out := ConfigFromApp(src)

	if _, ok := out.Profiles["unknown-base"]; ok {
		t.Fatal("a profile with an unknown Base must not survive resolution")
	}
	if _, ok := out.Profiles[string(config.AgentCoder)]; ok {
		t.Fatal("a profile colliding with a built-in agent name must not survive resolution")
	}
	if _, ok := out.Profiles["fine"]; !ok {
		t.Fatal("a well-formed profile must still resolve")
	}
}

// TestResolveProfileLooksUpByName exercises Config.resolveProfile directly.
func TestResolveProfileLooksUpByName(t *testing.T) {
	cfg := Config{Profiles: map[string]Profile{
		"backlog-assistant": {Name: "backlog-assistant", Base: config.AgentCoder},
	}}
	if _, ok := cfg.resolveProfile("backlog-assistant"); !ok {
		t.Fatal("expected the declared profile to resolve")
	}
	if _, ok := cfg.resolveProfile("nope"); ok {
		t.Fatal("an undeclared name must not resolve")
	}
}
