package extension

import "context"

// Provider request capability: how an extension adds headers to the requests
// the host sends to a model provider.
//
// The host already supports *static* per-account headers, written in the
// configuration next to the account they belong to. Anything that is fixed for
// the lifetime of an account — a tenant id, a build channel, a device label —
// belongs there and needs no extension at all.
//
// What configuration cannot express is a value that changes from one call to
// the next: which conversation this request belongs to, what kind of work it
// is doing, which budget it should be billed against. Those live in the
// request's context, not in a file, and that is the only reason this
// capability exists. An extension that only needs constants should use the
// configuration overlay to write them and stop here.

// ProviderRequest describes the outgoing call a decorator is being asked about.
// It is deliberately thin: it names the destination, not the payload. A
// decorator that wants to know more about the work in flight reads it from the
// context, which is the caller's own request context.
type ProviderRequest struct {
	// Provider is the provider identifier the host resolved for this call
	// ("anthropic", "openai", "ollama", ...).
	Provider string

	// Model is the API model id the request will name.
	Model string
}

// ProviderRequestDecorator is implemented by extensions that add headers to
// outgoing provider requests.
//
// DecorateProviderRequest is called once per HTTP request, on the goroutine
// making it, with that request's context. It must be cheap and must not block:
// it sits directly in front of a network call the user is waiting on.
//
// The returned map is a set of header names and values to add. The host
// applies them defensively:
//
//   - Security-relevant and transport-owned headers are never replaceable. A
//     decorator cannot set or overwrite the credential headers, the cookie
//     headers, the provider's own protocol headers, or the framing headers;
//     entries naming one are dropped and logged. This is enforced by the host
//     rather than promised by the contract, because a contract is not a
//     boundary.
//   - An empty name, or an error, means "add nothing to this request". An
//     error is logged and the request proceeds unchanged; it never fails the
//     call, because an optional capability must not be able to break the
//     host's ability to talk to its provider.
//
// With no decorator registered the host sends exactly the bytes it sent
// before this capability existed.
type ProviderRequestDecorator interface {
	Extension
	DecorateProviderRequest(ctx context.Context, req ProviderRequest) (map[string]string, error)
}
