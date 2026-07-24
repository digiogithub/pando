package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp"
)

// LSPProvider supplies LSP clients to the file tools and lazily activates the
// language server that handles a given file. It decouples the tools from the
// concrete app: *app.App implements it, returning thread-safe snapshots so the
// tools can iterate clients while servers are started or stopped concurrently.
type LSPProvider interface {
	// EnsureForFile lazily starts the language server(s) that handle the file's
	// type, if not already running and the binary is installed. It is a no-op
	// when on-demand activation is disabled. Equivalent to EnsureForFileTrigger
	// with config.LSPTriggerEdit.
	EnsureForFile(ctx context.Context, path string)
	// EnsureForFileTrigger is EnsureForFile with the reason for the activation.
	// The configuration decides which triggers may start a server, so a tool
	// that only reads a file must not claim it edited one.
	EnsureForFileTrigger(ctx context.Context, path string, trigger config.LSPTrigger)
	// WaitForFile blocks briefly while lazy startup settles so callers can use a
	// freshly spawned LSP client within the same request.
	WaitForFile(ctx context.Context, path string) map[string]*lsp.Client
	// ClientsForFile returns a snapshot of running clients that handle the file.
	ClientsForFile(path string) map[string]*lsp.Client
	// Clients returns a snapshot of all running clients.
	Clients() map[string]*lsp.Client
	// UnavailableReason explains, in terms a user can act on, why no ready
	// language server could be obtained for the file: a missing binary and its
	// install command, an install still in flight, or activation being off. It
	// returns an empty string when a server is available.
	UnavailableReason(path string) string
}

// LSPCatalogEntry describes one language server of the registry: how it is
// configured, whether its binary can be obtained, and what it is doing now.
type LSPCatalogEntry struct {
	Name        string
	Description string
	Command     string
	Languages   []string
	Filenames   []string
	// Configured reports an explicit [LSP.<name>] entry; OptIn a preset that
	// stays inactive until the user declares it.
	Configured bool
	OptIn      bool
	Disabled   bool
	Installing bool
	// Availability is "installed", "installable" or "manual", with
	// AvailabilityLabel its short human-readable form.
	Availability      string
	AvailabilityLabel string
	Reason            string
	Hint              string
	// RunState is "stopped", "starting", "ready" or "error".
	RunState string
}

// SetupLSPCatalog is the optional half of LSPProvider that describes the whole
// language-server catalogue. *app.App implements it; the static provider used
// by tests does not, and callers must degrade gracefully when the assertion
// fails.
type SetupLSPCatalog interface {
	LSPCatalog() []LSPCatalogEntry
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

func (s *staticLSPProvider) EnsureForFileTrigger(context.Context, string, config.LSPTrigger) {}

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

func (s *staticLSPProvider) UnavailableReason(path string) string {
	if len(s.ClientsForFile(path)) > 0 {
		return ""
	}
	return "no language server is running for this file"
}

func (s *staticLSPProvider) Clients() map[string]*lsp.Client {
	out := make(map[string]*lsp.Client, len(s.clients))
	for k, v := range s.clients {
		out[k] = v
	}
	return out
}
