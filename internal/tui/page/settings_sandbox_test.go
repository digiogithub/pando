package page

import (
	"context"
	"os/exec"
	"testing"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

type fakeTUISandboxWrapper struct{}

func (fakeTUISandboxWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: sandbox.BackendSeatbelt, Enforced: true,
		ProtectsNestedPaths: true, BlocksPorts: true}
}
func (fakeTUISandboxWrapper) Wrap(*exec.Cmd, sandbox.Policy) error { return nil }

// withSandboxTUIConfig loads a real config with an isolated HOME:
// config.UpdateSandbox writes the GLOBAL config file.
func withSandboxTUIConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	config.IsolateForTests(t)
	config.ClearOverlayProviders()
	viper.Reset()
	t.Cleanup(func() {
		config.ClearOverlayProviders()
		viper.Reset()
	})
	t.Cleanup(sandbox.SetDefaultForTests(fakeTUISandboxWrapper{}))
	cfg, err := config.Load(t.TempDir(), false)
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return cfg
}

func sandboxField(t *testing.T, sections []settings.Section, key string) settings.Field {
	t.Helper()
	for _, section := range sections {
		for _, field := range section.Fields {
			if field.Key == key {
				return field
			}
		}
	}
	t.Fatalf("%q not found", key)
	return settings.Field{}
}

func TestSandboxSectionDefaultsAndSave(t *testing.T) {
	cfg := withSandboxTUIConfig(t)
	sections := []settings.Section{buildSandboxSection(cfg)}

	if f := sandboxField(t, sections, "sandbox.enabled"); f.Value != "true" || f.Type != settings.FieldToggle {
		t.Fatalf("sandbox.enabled = %+v", f)
	}
	if f := sandboxField(t, sections, "sandbox.mode"); f.Value != "workspace-write" {
		t.Fatalf("sandbox.mode = %q", f.Value)
	}
	if f := sandboxField(t, sections, "sandbox.backend"); !f.ReadOnly || f.Value != "workspace-write (seatbelt)" {
		t.Fatalf("sandbox.backend = %+v", f)
	}

	for _, change := range []settings.Field{
		{Key: "sandbox.mode", Value: "strict"},
		{Key: "sandbox.autoAllowBash", Value: "false"},
		{Key: "sandbox.denyPaths", Value: "~/.ssh, ~/.aws"},
		{Key: "sandbox.extendTo.mcp", Value: "true"},
	} {
		if err := persistSetting(nil, change); err != nil {
			t.Fatalf("persist %s: %v", change.Key, err)
		}
	}
	got := config.Get().Sandbox
	if got.Mode != "strict" || !got.AutoAllowBashDisabled || len(got.DenyPaths) != 2 || len(got.ExtendTo) != 1 {
		t.Fatalf("sandbox config = %+v", got)
	}
	if sandbox.Current().Mode != sandbox.ModeStrict {
		t.Fatalf("the next command would not see the new mode: %q", sandbox.Current().Mode)
	}

	if err := persistSetting(nil, settings.Field{Key: "sandbox.enabled", Value: "false"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !config.Get().Sandbox.Disabled || sandbox.Current().Enabled() {
		t.Fatal("disabling from the TUI did not turn the sandbox off")
	}
	if f := sandboxField(t, []settings.Section{buildSandboxSection(cfg)}, "sandbox.backend"); f.Value != "off" {
		t.Fatalf("status after disable = %q, want off", f.Value)
	}
}

func TestSandboxSectionLockedFields(t *testing.T) {
	cfg := withSandboxTUIConfig(t)
	config.RegisterOverlayProvider(config.OverlayProviderFunc(func(context.Context) (config.Overlay, error) {
		return config.Overlay{Locked: []string{"sandbox.disabled", "sandbox.autoAllowBashDisabled"}}, nil
	}))
	if err := config.ApplyOverlays(context.Background()); err != nil {
		t.Fatalf("ApplyOverlays: %v", err)
	}

	sections := applyFieldPolicy(nil, []settings.Section{buildSandboxSection(cfg)})
	for _, key := range []string{"sandbox.enabled", "sandbox.mode", "sandbox.autoAllowBash"} {
		if !sandboxField(t, sections, key).Locked {
			t.Fatalf("%s should render locked", key)
		}
	}
	if sandboxField(t, sections, "sandbox.network").Locked {
		t.Fatal("sandbox.network is not locked")
	}
	if err := persistSetting(nil, settings.Field{Key: "sandbox.enabled", Value: "false"}); err == nil {
		t.Fatal("a locked sandbox.enabled was saved")
	}
	if err := persistSetting(nil, settings.Field{Key: "sandbox.network", Value: "restricted"}); err != nil {
		t.Fatalf("unlocked sandbox.network refused: %v", err)
	}
	if config.Get().Sandbox.Disabled || config.Get().Sandbox.Network != "restricted" {
		t.Fatalf("sandbox config = %+v", config.Get().Sandbox)
	}
}
