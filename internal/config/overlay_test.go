package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

// resetOverlayState returns the package globals to their zero state around a
// test. The configuration layer is a process-wide singleton, so every test that
// touches it has to put it back.
func resetOverlayState(t *testing.T) {
	t.Helper()
	ClearOverlayProviders()
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		ClearOverlayProviders()
		cfg = nil
		viper.Reset()
	})
}

func TestBuildOverlayPatchMergeSemantics(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		overlay  map[string]any
		additive []string
		want     map[string]any
		changed  []string
	}{
		{
			name:    "scalar replaces",
			base:    map[string]any{"tui": map[string]any{"theme": "dark", "nerdfonts": true}},
			overlay: map[string]any{"tui": map[string]any{"theme": "corporate"}},
			want:    map[string]any{"tui": map[string]any{"theme": "corporate"}},
			changed: []string{"tui.theme"},
		},
		{
			name:    "scalar equal to the loaded value is not a change",
			base:    map[string]any{"tui": map[string]any{"theme": "dark"}},
			overlay: map[string]any{"tui": map[string]any{"theme": "dark"}},
			want:    map[string]any{"tui": map[string]any{"theme": "dark"}},
			changed: nil,
		},
		{
			name:    "key casing follows the config file, not viper",
			base:    map[string]any{"tui": map[string]any{"theme": "dark"}},
			overlay: map[string]any{"TUI": map[string]any{"Theme": "corporate"}},
			want:    map[string]any{"TUI": map[string]any{"Theme": "corporate"}},
			changed: []string{"TUI.Theme"},
		},
		{
			name:    "map merges key by key",
			base:    map[string]any{"agents": map[string]any{"coder": map[string]any{"model": "a", "maxTokens": 100.0}}},
			overlay: map[string]any{"agents": map[string]any{"coder": map[string]any{"model": "b"}}},
			want:    map[string]any{"agents": map[string]any{"coder": map[string]any{"model": "b"}}},
			changed: []string{"agents.coder.model"},
		},
		{
			name:    "list replaces by default",
			base:    map[string]any{"contextPaths": []any{"a", "b"}},
			overlay: map[string]any{"contextPaths": []any{"c"}},
			want:    map[string]any{"contextPaths": []any{"c"}},
			changed: []string{"contextPaths"},
		},
		{
			name:     "additive list is unioned, loaded values first",
			base:     map[string]any{"contextPaths": []any{"a", "b"}},
			overlay:  map[string]any{"contextPaths": []any{"b", "c"}},
			additive: []string{"contextPaths"},
			want:     map[string]any{"contextPaths": []any{"a", "b", "c"}},
			changed:  []string{"contextPaths"},
		},
		{
			name:     "additive list that adds nothing is not a change",
			base:     map[string]any{"contextPaths": []any{"a", "b"}},
			overlay:  map[string]any{"contextPaths": []any{"a"}},
			additive: []string{"contextPaths"},
			want:     map[string]any{"contextPaths": []any{"a", "b"}},
			changed:  nil,
		},
		{
			name: "lists of identified objects merge by id",
			base: map[string]any{"providerAccounts": []any{
				map[string]any{"id": "personal", "type": "anthropic"},
				map[string]any{"id": "shared", "type": "openai", "apiKey": "old"},
			}},
			overlay: map[string]any{"providerAccounts": []any{
				map[string]any{"id": "shared", "type": "openai", "apiKey": "new"},
			}},
			want: map[string]any{"providerAccounts": []any{
				map[string]any{"id": "shared", "type": "openai", "apiKey": "new"},
				map[string]any{"id": "personal", "type": "anthropic"},
			}},
			changed: []string{"providerAccounts"},
		},
		{
			name:    "a key the overlay does not mention stays out of the patch",
			base:    map[string]any{"tui": map[string]any{"theme": "dark"}, "debug": true},
			overlay: map[string]any{"debug": false},
			want:    map[string]any{"debug": false},
			changed: []string{"debug"},
		},
		{
			name:    "a key absent from the base counts as a change",
			base:    map[string]any{},
			overlay: map[string]any{"skills": map[string]any{"enabled": true}},
			want:    map[string]any{"skills": map[string]any{"enabled": true}},
			changed: []string{"skills.enabled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			patch, changed := buildOverlayPatch(tc.base, tc.overlay, tc.additive, nil)
			if !reflect.DeepEqual(patch, tc.want) {
				t.Fatalf("patch = %#v, want %#v", patch, tc.want)
			}
			if !reflect.DeepEqual(normalizePaths(changed), normalizePaths(tc.changed)) {
				t.Fatalf("changed = %v, want %v", changed, tc.changed)
			}
		})
	}
}

func TestPathMatching(t *testing.T) {
	tests := []struct {
		path, prefix string
		wantPrefix   bool
		wantOverlap  bool
	}{
		{"tui.theme", "tui.theme", true, true},
		{"TUI.Theme", "tui.theme", true, true},
		{"mcpServers.github.command", "mcpServers", true, true},
		{"mcpServers", "mcpServers.github.command", false, true},
		{"tuiExtra.theme", "tui", false, false},
		{"tui", "", false, false},
	}
	for _, tc := range tests {
		if got := pathHasPrefix(tc.path, tc.prefix); got != tc.wantPrefix {
			t.Errorf("pathHasPrefix(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.wantPrefix)
		}
		if got := pathsOverlap(tc.path, tc.prefix); got != tc.wantOverlap {
			t.Errorf("pathsOverlap(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.wantOverlap)
		}
	}
}

func TestChangedPaths(t *testing.T) {
	before := map[string]any{
		"tui":   map[string]any{"theme": "dark", "nerdFonts": true},
		"debug": false,
		"agents": map[string]any{
			"coder": map[string]any{"model": "a"},
		},
	}
	after := map[string]any{
		"tui":   map[string]any{"theme": "light", "nerdFonts": true},
		"debug": false,
		"agents": map[string]any{
			"coder": map[string]any{"model": "a"},
			"task":  map[string]any{"model": "b"},
		},
		"logFile": "",
	}

	got := normalizePaths(changedPaths(before, after, nil))
	want := []string{"agents.task.model", "tui.theme"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedPaths = %v, want %v", got, want)
	}
}

func TestLockedKeyErrorIsErrKeyLocked(t *testing.T) {
	err := error(&LockedKeyError{Key: "tui.theme"})
	if !errors.Is(err, ErrKeyLocked) {
		t.Fatal("errors.Is(err, ErrKeyLocked) = false, want true")
	}
	var typed *LockedKeyError
	if !errors.As(err, &typed) || typed.Key != "tui.theme" {
		t.Fatalf("errors.As did not recover the key, got %#v", typed)
	}
	if got := err.Error(); got == "" || got == ErrKeyLocked.Error() {
		t.Fatalf("Error() = %q, want a message naming the key", got)
	}
}

// writeProjectConfig writes a project config file and returns its directory.
func writeProjectConfig(t *testing.T, doc map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pando.json"), raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

func TestLoadAppliesOverlayAndRecordsLockedKeys(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := writeProjectConfig(t, map[string]any{
		"tui":          map[string]any{"theme": "dark"},
		"contextPaths": []string{"local.md"},
	})

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Source:   "test",
			Values:   map[string]any{"tui": map[string]any{"theme": "corporate"}, "contextPaths": []any{"policy.md"}},
			Locked:   []string{"tui.theme"},
			Additive: []string{"contextPaths"},
		}, nil
	}))

	events := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(events)
	t.Cleanup(func() { Bus.Unsubscribe(events) })

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want the overlay value", loaded.TUI.Theme)
	}
	if !contains(loaded.ContextPaths, "local.md") || !contains(loaded.ContextPaths, "policy.md") {
		t.Fatalf("contextPaths = %v, want the union of local and overlay entries", loaded.ContextPaths)
	}
	if got := LockedKeys(); !reflect.DeepEqual(got, []string{"tui.theme"}) {
		t.Fatalf("LockedKeys = %v, want [tui.theme]", got)
	}
	if !IsKeyLocked("tui.theme") || !IsKeyLocked("TUI.Theme") {
		t.Fatal("IsKeyLocked did not report the locked key")
	}
	if IsKeyLocked("tui.nerdFonts") {
		t.Fatal("IsKeyLocked reported an unlocked sibling as locked")
	}

	select {
	case ev := <-events:
		if ev.Event != EventOverlayApplied {
			t.Fatalf("event = %q, want %q", ev.Event, EventOverlayApplied)
		}
		if !contains(ev.ChangedKeys, "tui.theme") {
			t.Fatalf("changed keys = %v, want tui.theme among them", ev.ChangedKeys)
		}
	default:
		t.Fatal("no overlay_applied event was published")
	}
}

func TestReloadReappliesOverlay(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	theme := "corporate"
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{Values: map[string]any{"tui": map[string]any{"theme": theme}}, Locked: []string{"tui.theme"}}, nil
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want corporate", loaded.TUI.Theme)
	}

	// A reload must ask the provider again, not fall back to the file.
	theme = "corporate-v2"
	if err := ApplyOverlays(context.Background()); err != nil {
		t.Fatalf("ApplyOverlays: %v", err)
	}
	if Get().TUI.Theme != "corporate-v2" {
		t.Fatalf("after reapply tui.theme = %q, want corporate-v2", Get().TUI.Theme)
	}
	// The pointer callers captured at load time must still be the live config.
	if loaded.TUI.Theme != "corporate-v2" {
		t.Fatalf("reload replaced the config pointer instead of updating it in place: %q", loaded.TUI.Theme)
	}
	if got := LockedKeys(); !reflect.DeepEqual(got, []string{"tui.theme"}) {
		t.Fatalf("LockedKeys after reapply = %v, want [tui.theme]", got)
	}
}

func TestOverlayProviderFailureLeavesConfigurationLoaded(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{}, errors.New("policy server unreachable")
	}))
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		panic("provider is broken")
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.TUI.Theme != "dark" {
		t.Fatalf("tui.theme = %q, want the file value", loaded.TUI.Theme)
	}
	if got := LockedKeys(); len(got) != 0 {
		t.Fatalf("LockedKeys = %v, want none", got)
	}
}

func TestUpdateRefusesLockedKeyAndAllowsOthers(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
			Locked: []string{"tui.theme", "internalTools"},
		}, nil
	}))

	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	err := UpdateTheme("hotdogstand")
	if !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("UpdateTheme error = %v, want a locked-key refusal", err)
	}

	itCfg := Get().InternalTools
	itCfg.BraveAPIKey = "local-key"
	if err := UpdateInternalTools(itCfg); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("UpdateInternalTools error = %v, want a locked-key refusal on a locked subtree", err)
	}

	// An unlocked key in the same section still writes.
	if err := UpdateNerdFonts(true); err != nil {
		t.Fatalf("UpdateNerdFonts on an unlocked key: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".pando.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	tui, _ := onDisk["tui"].(map[string]any)
	if tui["theme"] != "dark" {
		t.Fatalf("on-disk tui.theme = %v, want the untouched file value", tui["theme"])
	}
	if tui["nerdFonts"] != true {
		t.Fatalf("on-disk tui.nerdFonts = %v, want the unlocked write to have landed", tui["nerdFonts"])
	}
}

func TestErrIfLockedRefusesBeforeAnyWrite(t *testing.T) {
	resetOverlayState(t)

	overlayMu.Lock()
	lockedKeys = []string{"mcpServers"}
	overlayMu.Unlock()

	if err := ErrIfLocked("tui.theme"); err != nil {
		t.Fatalf("ErrIfLocked on an unlocked key = %v, want nil", err)
	}
	err := ErrIfLocked("tui.theme", "mcpServers.github.command")
	if !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("ErrIfLocked = %v, want a locked-key refusal", err)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
