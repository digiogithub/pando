package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

// withCavemanTUIConfig points the config at a throwaway working directory with
// its own .pando.toml, so saveCaveman writes there and never touches the
// developer's real ~/.pando.toml.
func withCavemanTUIConfig(t *testing.T, defaultMode string) (*config.Config, string) {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(file, []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	prev := config.Get()
	cfg := &config.Config{
		WorkingDir: dir,
		Debug:      true,
		Caveman:    config.CavemanConfig{DefaultMode: defaultMode},
	}
	config.SetForTests(cfg)
	t.Cleanup(func() { config.SetForTests(prev) })

	return cfg, file
}

func cavemanField(t *testing.T, cfg *config.Config) settings.Field {
	t.Helper()
	section := buildTokenOptimizationSection(cfg)
	for _, field := range section.Fields {
		if field.Key == "caveman.defaultMode" {
			return field
		}
	}
	t.Fatalf("caveman.defaultMode not found in the %q section", section.Title)
	return settings.Field{}
}

func TestBuildTokenOptimizationSectionExposesCaveman(t *testing.T) {
	cfg, _ := withCavemanTUIConfig(t, "ultra")

	field := cavemanField(t, cfg)
	if field.Type != settings.FieldSelect {
		t.Errorf("field type = %v, want FieldSelect", field.Type)
	}
	if field.Value != "ultra" {
		t.Errorf("field value = %q, want %q", field.Value, "ultra")
	}
	if want := []string{"off", "lite", "full", "ultra"}; strings.Join(field.Options, ",") != strings.Join(want, ",") {
		t.Errorf("options = %v, want %v", field.Options, want)
	}
	// The output-only caveat must be visible; brevity never buys speed at the
	// cost of verification, and it cannot shrink input tokens.
	if !strings.Contains(field.Hint, "input and reasoning tokens are not reduced") {
		t.Errorf("hint must carry the output-only caveat, got %q", field.Hint)
	}
}

// An off default has to render as "off", not as the empty string the config stores.
func TestBuildTokenOptimizationSectionRendersOffCaveman(t *testing.T) {
	cfg, _ := withCavemanTUIConfig(t, "")

	if got := cavemanField(t, cfg).Value; got != "off" {
		t.Errorf("field value = %q, want %q", got, "off")
	}
}

func TestSaveCavemanPersistsLevel(t *testing.T) {
	_, file := withCavemanTUIConfig(t, "")

	if err := saveCaveman(settings.Field{Key: "caveman.defaultMode", Value: "lite"}); err != nil {
		t.Fatalf("saveCaveman: %v", err)
	}
	if got := config.Get().CavemanDefaultMode(); got != "lite" {
		t.Errorf("in-memory default = %q, want %q", got, "lite")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	if !strings.Contains(string(data), "lite") {
		t.Errorf("expected the level to be persisted, got:\n%s", data)
	}

	// Readback: the rebuilt section shows what was just saved.
	if got := cavemanField(t, config.Get()).Value; got != "lite" {
		t.Errorf("field value after save = %q, want %q", got, "lite")
	}
}

func TestSaveCavemanOffClearsLevel(t *testing.T) {
	withCavemanTUIConfig(t, "full")

	if err := saveCaveman(settings.Field{Key: "caveman.defaultMode", Value: caveman.ModeOff.String()}); err != nil {
		t.Fatalf("saveCaveman(off): %v", err)
	}
	if got := config.Get().CavemanDefaultMode(); got != "" {
		t.Errorf("default = %q, want empty", got)
	}
}

func TestSaveCavemanRejectsUnknownLevel(t *testing.T) {
	withCavemanTUIConfig(t, "full")

	if err := saveCaveman(settings.Field{Key: "caveman.defaultMode", Value: "shouty"}); err == nil {
		t.Fatal("expected an error for an unsupported level")
	}
	// The rejected save must not silently disable the mode.
	if got := config.Get().CavemanDefaultMode(); got != "full" {
		t.Errorf("default changed to %q on a rejected save, want %q", got, "full")
	}
}

func TestPersistSettingRoutesCavemanKey(t *testing.T) {
	withCavemanTUIConfig(t, "")

	// The caveman branch dispatches on the key alone; the app is only needed by
	// other setting groups.
	if err := persistSetting(nil, settings.Field{Key: "caveman.defaultMode", Value: "ultra"}); err != nil {
		t.Fatalf("persistSetting: %v", err)
	}
	if got := config.Get().CavemanDefaultMode(); got != "ultra" {
		t.Errorf("default = %q, want %q", got, "ultra")
	}
}
