package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/lsp"
)

// LSPProvider supplies LSP clients to the file tools and lazily activates the
// language server that handles a given file. It decouples the tools from the
// concrete app: *app.App implements it, returning thread-safe snapshots so the
// tools can iterate clients while servers are started or stopped concurrently.
type LSPProvider interface {
	// EnsureForFile lazily starts the language server(s) that handle the file's
	// type, if not already running and the binary is installed. It is a no-op
	// when on-demand activation is disabled.
	EnsureForFile(ctx context.Context, path string)
	// WaitForFile blocks briefly while lazy startup settles so callers can use a
	// freshly spawned LSP client within the same request.
	WaitForFile(ctx context.Context, path string) map[string]*lsp.Client
	// ClientsForFile returns a snapshot of running clients that handle the file.
	ClientsForFile(path string) map[string]*lsp.Client
	// Clients returns a snapshot of all running clients.
	Clients() map[string]*lsp.Client
}

// staticLSPProvider adapts a fixed client map to LSPProvider without any lazy
// activation. Useful for tests and callers that only have a client map.
type staticLSPProvider struct {
	clients map[string]*lsp.Client
}

// NewStaticLSPProvider wraps a fixed map of LSP clients as an LSPProvider.
func NewStaticLSPProvider(clients map[string]*lsp.Client) LSPProvider {
	return &staticLSPProvider{clients: clients}
}

func (s *staticLSPProvider) EnsureForFile(context.Context, string) {}

func (s *staticLSPProvider) WaitForFile(_ context.Context, path string) map[string]*lsp.Client {
	return s.ClientsForFile(path)
}

func (s *staticLSPProvider) ClientsForFile(path string) map[string]*lsp.Client {
	out := make(map[string]*lsp.Client)
	for name, c := range s.clients {
		if c.HandlesFile(path) {
			out[name] = c
		}
	}
	return out
}

func (s *staticLSPProvider) Clients() map[string]*lsp.Client {
	out := make(map[string]*lsp.Client, len(s.clients))
	for k, v := range s.clients {
		out[k] = v
	}
	return out
}
