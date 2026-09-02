package extension

import (
	"context"
	"errors"
	"testing"
)

// policyExtension is the worked example of the configuration overlay
// capability: an extension that imposes a configuration document and freezes
// the keys it owns. A real one would fetch the document and cache it; this one
// keeps it in a field, which is the same shape from core's point of view.
type policyExtension struct {
	info    Info
	overlay ConfigOverlay
	err     error
	host    HostServices
	calls   int
}

func (p *policyExtension) ExtensionInfo() Info { return p.info }

func (p *policyExtension) Provision(_ context.Context, host HostServices) error {
	p.host = host
	return nil
}

func (p *policyExtension) ConfigOverlay(context.Context) (ConfigOverlay, error) {
	p.calls++
	if p.err != nil {
		return ConfigOverlay{}, p.err
	}
	return p.overlay, nil
}

func registerPolicy(r *Registry, id ID, ov ConfigOverlay, err error) *policyExtension {
	inst := &policyExtension{
		info:    Info{ID: id, Name: string(id), Version: "1.0.0"},
		overlay: ov,
		err:     err,
	}
	inst.info.New = func() Extension { return inst }
	r.Register(inst)
	return inst
}

func TestConfigOverlayProviderIsDiscoveredAsCapability(t *testing.T) {
	reg := NewRegistry()
	var calls []string
	registerRecorder(reg, "tools.plain", &calls, nil)
	policy := registerPolicy(reg, "policy.corp", ConfigOverlay{
		Source: "policy",
		Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
		Locked: []string{"tui.theme"},
	}, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	providers := Capability[ConfigOverlayProvider](m)
	if len(providers) != 1 {
		t.Fatalf("Capability returned %d providers, want 1", len(providers))
	}
	if providers[0].ExtensionInfo().ID != "policy.corp" {
		t.Fatalf("provider ID = %q, want policy.corp", providers[0].ExtensionInfo().ID)
	}

	ov, err := providers[0].ConfigOverlay(context.Background())
	if err != nil {
		t.Fatalf("ConfigOverlay: %v", err)
	}
	if ov.Values["tui"].(map[string]any)["theme"] != "corporate" {
		t.Fatalf("overlay values = %#v, want the declared document", ov.Values)
	}
	if len(ov.Locked) != 1 || ov.Locked[0] != "tui.theme" {
		t.Fatalf("locked = %v, want [tui.theme]", ov.Locked)
	}
	if policy.calls != 1 {
		t.Fatalf("provider was called %d times, want once", policy.calls)
	}
}

func TestConfigOverlayProviderErrorIsTheExtensionsToReport(t *testing.T) {
	reg := NewRegistry()
	registerPolicy(reg, "policy.corp", ConfigOverlay{}, errors.New("cache empty"))

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// A provider that cannot answer is still loaded: the failure belongs to
	// the overlay, not to the extension's lifecycle.
	if !m.Loaded("policy.corp") {
		t.Fatal("extension is not loaded")
	}
	if _, err := Capability[ConfigOverlayProvider](m)[0].ConfigOverlay(context.Background()); err == nil {
		t.Fatal("ConfigOverlay error = nil, want the provider's error")
	}
}

// stubConfigView is the smallest ConfigView an out-of-tree test needs, and
// exists here to keep the interface honest: adding a method to it breaks every
// such stub, so the compiler tells us when the contract widened.
type stubConfigView struct {
	locked []string
}

func (stubConfigView) WorkingDir() string        { return "/tmp/project" }
func (stubConfigView) DataDir() string           { return "/tmp/project/.pando/data" }
func (stubConfigView) Debug() bool               { return false }
func (stubConfigView) Lookup(string) (any, bool) { return nil, false }
func (s stubConfigView) LockedKeys() []string    { return s.locked }

type stubOverlayController struct{ calls int }

func (c *stubOverlayController) ReapplyOverlays(context.Context) error {
	c.calls++
	return nil
}

func TestHostServicesExposeLockStateAndReapply(t *testing.T) {
	ctrl := &stubOverlayController{}
	host := HostServices{
		Config:         stubConfigView{locked: []string{"tui.theme"}},
		ConfigOverlays: ctrl,
	}

	if got := host.Config.LockedKeys(); len(got) != 1 || got[0] != "tui.theme" {
		t.Fatalf("LockedKeys = %v, want [tui.theme]", got)
	}
	if host.ConfigOverlays == nil {
		t.Fatal("ConfigOverlays is nil, want the host controller")
	}
	if err := host.ConfigOverlays.ReapplyOverlays(context.Background()); err != nil {
		t.Fatalf("ReapplyOverlays: %v", err)
	}
	if ctrl.calls != 1 {
		t.Fatalf("controller called %d times, want once", ctrl.calls)
	}
}
