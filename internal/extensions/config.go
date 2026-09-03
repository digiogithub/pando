package extensions

import (
	"context"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the configuration overlay capability.
//
// Two adapters, both thin on purpose. overlayProvider turns an extension that
// implements extension.ConfigOverlayProvider into something internal/config
// understands without either package importing the other, and adds the panic
// containment every core-to-extension call gets. overlayController is the
// reverse direction: the handle an extension uses to say its document changed.

// overlayProvider adapts one extension to config.OverlayProvider.
type overlayProvider struct {
	id  extension.ID
	ext extension.ConfigOverlayProvider
}

// ConfigOverlay asks the extension for its document. A panic is contained and
// reported as an error, which config treats as "this provider has nothing to
// say" and keeps the configuration it already loaded: an optional capability
// must never be able to stop Pando from starting.
func (p overlayProvider) ConfigOverlay(ctx context.Context) (ov config.Overlay, err error) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension call panicked, ignoring its result",
				"extension", ownerName(p.id), "call", "ConfigOverlay", "panic", r)
			ov, err = config.Overlay{}, errFromPanic(r)
		}
	}()

	got, err := p.ext.ConfigOverlay(ctx)
	if err != nil {
		return config.Overlay{}, err
	}
	source := got.Source
	if source == "" {
		source = string(p.id)
	}
	return config.Overlay{
		Source:   source,
		Values:   got.Values,
		Locked:   got.Locked,
		Additive: got.Additive,
	}, nil
}

// overlayController implements extension.ConfigOverlayController.
type overlayController struct{}

// ReapplyOverlays re-runs configuration load. It must not be called from
// inside ConfigOverlay: the reapply is serialised, so a provider that called
// it while answering would wait for itself.
func (overlayController) ReapplyOverlays(ctx context.Context) error {
	return config.ApplyOverlays(ctx)
}

// RequestReload is the coalesced form. Bursts of requests collapse into a
// single reload and every caller that joined it gets the same result, which is
// what keeps an extension driven by a remote stream from making the host
// re-read its configuration once per message.
func (overlayController) RequestReload(ctx context.Context, reason string) error {
	return config.RequestReload(ctx, reason)
}

// RegisterConfigOverlays registers every loaded extension that provides a
// configuration overlay and applies the result.
//
// Overlays are collected in extension load order, so an extension that depends
// on another (RequiresExtensions) overrides it, which is the same precedence
// the rest of the extension system uses.
//
// Applying reloads the configuration. Subsystems that read config.Get() see
// the overlaid values immediately; subsystems that copied a value out at
// construction time learn about the change from the config event bus, where an
// overlay publishes an "overlay_applied" event naming the keys it changed.
func RegisterConfigOverlays(ctx context.Context, mgr *extension.Manager) error {
	providers := extension.Capability[extension.ConfigOverlayProvider](mgr)
	if len(providers) == 0 {
		// Nothing declares an overlay: drop any lock list left by a previous
		// manager so keys do not stay frozen after the extension went away.
		if config.HasOverlayProviders() {
			config.ClearOverlayProviders()
			return config.ApplyOverlays(ctx)
		}
		return nil
	}

	config.ClearOverlayProviders()
	for _, p := range providers {
		config.RegisterOverlayProvider(overlayProvider{id: p.ExtensionInfo().ID, ext: p})
	}
	return config.ApplyOverlays(ctx)
}
