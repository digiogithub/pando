package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/pkg/extension"
)

// overlayExtension is an extension that imposes configuration, the shape a
// policy-driven deployment uses.
type overlayExtension struct {
	info    extension.Info
	overlay extension.ConfigOverlay
	err     error
	panics  bool
}

func (o *overlayExtension) ExtensionInfo() extension.Info { return o.info }

func (o *overlayExtension) ConfigOverlay(context.Context) (extension.ConfigOverlay, error) {
	if o.panics {
		panic("overlay provider is broken")
	}
	if o.err != nil {
		return extension.ConfigOverlay{}, o.err
	}
	return o.overlay, nil
}

func newOverlayExtension(id extension.ID, ov extension.ConfigOverlay) *overlayExtension {
	inst := &overlayExtension{
		info:    extension.Info{ID: id, Name: string(id), Version: "1.0.0"},
		overlay: ov,
	}
	inst.info.New = func() extension.Extension { return inst }
	return inst
}

func quietManagerWith(t *testing.T, insts ...extension.Extension) *extension.Manager {
	t.Helper()
	reg := extension.NewRegistry()
	for _, inst := range insts {
		reg.Register(inst)
	}
	return extension.NewManager(extension.Options{
		Registry: reg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// projectWithConfig writes a project config file and isolates the global one,
// so the test never reads the config of whoever runs it.
func projectWithConfig(t *testing.T, doc map[string]any) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
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

func TestRegisterConfigOverlaysAppliesTheDocument(t *testing.T) {
	dir := projectWithConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	ext := newOverlayExtension("policy.test", extension.ConfigOverlay{
		Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
		Locked: []string{"tui.theme"},
	})
	mgr := quietManagerWith(t, ext)

	ctx := context.Background()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, mgr); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}

	if got := config.Get().TUI.Theme; got != "corporate" {
		t.Fatalf("tui.theme = %q, want the overlay value", got)
	}
	if got := config.LockedKeys(); len(got) != 1 || got[0] != "tui.theme" {
		t.Fatalf("LockedKeys = %v, want [tui.theme]", got)
	}

	// The read-only view an extension holds reports the same lock list.
	view := configView{cfg: config.Get()}
	if got := view.LockedKeys(); len(got) != 1 || got[0] != "tui.theme" {
		t.Fatalf("ConfigView.LockedKeys = %v, want [tui.theme]", got)
	}

	// And the write path refuses to change it.
	if err := config.UpdateTheme("hotdogstand"); !errors.Is(err, config.ErrKeyLocked) {
		t.Fatalf("UpdateTheme = %v, want a locked-key refusal", err)
	}
}

func TestRegisterConfigOverlaysContainsAPanickingProvider(t *testing.T) {
	dir := projectWithConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	ext := newOverlayExtension("policy.broken", extension.ConfigOverlay{})
	ext.panics = true
	mgr := quietManagerWith(t, ext)

	ctx := context.Background()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, mgr); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}

	if got := config.Get().TUI.Theme; got != "dark" {
		t.Fatalf("tui.theme = %q, want the file value to survive a broken provider", got)
	}
	if got := config.LockedKeys(); len(got) != 0 {
		t.Fatalf("LockedKeys = %v, want none", got)
	}
}

func TestRegisterConfigOverlaysClearsLocksWhenNoProviderRemains(t *testing.T) {
	dir := projectWithConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	ctx := context.Background()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	withProvider := quietManagerWith(t, newOverlayExtension("policy.test", extension.ConfigOverlay{
		Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
		Locked: []string{"tui.theme"},
	}))
	if err := withProvider.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, withProvider); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}
	if !config.IsKeyLocked("tui.theme") {
		t.Fatal("key is not locked after the provider was registered")
	}

	// A manager with no overlay provider must release the keys the previous
	// one froze, otherwise removing an extension would leave settings dead.
	empty := quietManagerWith(t)
	if err := empty.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, empty); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}
	if config.IsKeyLocked("tui.theme") {
		t.Fatal("key is still locked after the provider went away")
	}
	if got := config.Get().TUI.Theme; got != "dark" {
		t.Fatalf("tui.theme = %q, want the file value back", got)
	}
}

func TestOverlayControllerReapplies(t *testing.T) {
	dir := projectWithConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	ext := newOverlayExtension("policy.test", extension.ConfigOverlay{
		Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
	})
	mgr := quietManagerWith(t, ext)

	ctx := context.Background()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, mgr); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}

	// The extension changes what its document says and asks for a reapply,
	// exactly as it would after a new policy arrived.
	ext.overlay.Values = map[string]any{"tui": map[string]any{"theme": "corporate-v2"}}
	if err := (overlayController{}).ReapplyOverlays(ctx); err != nil {
		t.Fatalf("ReapplyOverlays: %v", err)
	}
	if got := config.Get().TUI.Theme; got != "corporate-v2" {
		t.Fatalf("tui.theme = %q, want the refreshed overlay value", got)
	}
}

func TestOverlayControllerRequestReload(t *testing.T) {
	dir := projectWithConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	ext := newOverlayExtension("policy.test", extension.ConfigOverlay{
		Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
	})
	mgr := quietManagerWith(t, ext)

	ctx := context.Background()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("manager Load: %v", err)
	}
	if err := RegisterConfigOverlays(ctx, mgr); err != nil {
		t.Fatalf("RegisterConfigOverlays: %v", err)
	}

	// The coalesced path an extension uses when a remote notification arrives:
	// several requests, one reload, everyone told how it went.
	ext.overlay.Values = map[string]any{"tui": map[string]any{"theme": "corporate-v3"}}
	var ctrl extension.ConfigOverlayController = overlayController{}
	for i := 0; i < 3; i++ {
		if err := ctrl.RequestReload(ctx, "new generation"); err != nil {
			t.Fatalf("RequestReload: %v", err)
		}
	}
	if got := config.Get().TUI.Theme; got != "corporate-v3" {
		t.Fatalf("tui.theme = %q, want the refreshed overlay value", got)
	}
}
