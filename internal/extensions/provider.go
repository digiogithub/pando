package extensions

import (
	"context"

	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the provider request capability.
//
// The adapter turns the loaded ProviderRequestDecorator extensions into the
// single function internal/llm/provider installs as SDK middleware. It runs on
// the request path, in front of a network call somebody is waiting on, so
// everything it does is bounded and nothing it does can fail the call:
//
//   - a panicking decorator is contained and contributes nothing;
//   - an erroring decorator is logged and contributes nothing;
//   - a decorator that names a header the host owns has that entry dropped,
//     with the extension named in the log. The same check runs again where the
//     headers are applied, because a boundary that is only checked on one side
//     is not a boundary.
//
// There is no timeout here. A decorator is contractually a pure function over
// the context it is handed, and the call it decorates already carries the
// caller's own deadline; adding a second one would only hide a bug behind a
// slower request.

// ProviderRequestDecorator returns the per-request header hook for the loaded
// extensions, or nil when nothing implements the capability. Passing nil to
// provider.SetRequestDecorator is the same as never having called it, so the
// caller can wire the result unconditionally.
func ProviderRequestDecorator(mgr *extension.Manager) provider.RequestDecorator {
	decorators := extension.Capability[extension.ProviderRequestDecorator](mgr)
	if len(decorators) == 0 {
		return nil
	}
	return func(ctx context.Context, info provider.RequestInfo) map[string]string {
		req := extension.ProviderRequest{Provider: info.Provider, Model: info.Model}
		var out map[string]string
		for _, d := range decorators {
			headers := callProviderDecorator(ctx, d, req)
			for name, value := range headers {
				if provider.IsProtectedRequestHeader(name) {
					logging.Warn("Extension tried to set a provider header the host owns, dropping it",
						"extension", ownerName(d.ExtensionInfo().ID), "header", name)
					continue
				}
				if out == nil {
					out = make(map[string]string, len(headers))
				}
				// Later extensions in load order win, which is the same
				// precedence the rest of the extension system uses.
				out[name] = value
			}
		}
		return out
	}
}

// callProviderDecorator runs one decorator, containing a panic and swallowing
// an error: neither may cost the host its ability to reach the provider.
func callProviderDecorator(ctx context.Context, d extension.ProviderRequestDecorator, req extension.ProviderRequest) (headers map[string]string) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension call panicked, ignoring its result",
				"extension", ownerName(d.ExtensionInfo().ID), "call", "DecorateProviderRequest", "panic", r)
			headers = nil
		}
	}()

	got, err := d.DecorateProviderRequest(ctx, req)
	if err != nil {
		logging.Warn("Extension failed to decorate a provider request, sending it undecorated",
			"extension", ownerName(d.ExtensionInfo().ID), "error", err)
		return nil
	}
	return got
}
