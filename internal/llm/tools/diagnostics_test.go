package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp"
)

// stubLSPProvider reports no clients at all and a fixed unavailability reason,
// mirroring what the app returns when a language server is missing.
type stubLSPProvider struct {
	reason string
	// otherClients stands for servers running for unrelated languages.
	otherClients map[string]*lsp.Client
	ensured      []config.LSPTrigger
}

func (s *stubLSPProvider) EnsureForFile(context.Context, string) {}

func (s *stubLSPProvider) EnsureForFileTrigger(_ context.Context, _ string, trigger config.LSPTrigger) {
	s.ensured = append(s.ensured, trigger)
}

func (s *stubLSPProvider) WaitForFile(context.Context, string) map[string]*lsp.Client {
	return nil
}

func (s *stubLSPProvider) ClientsForFile(string) map[string]*lsp.Client { return nil }

func (s *stubLSPProvider) Clients() map[string]*lsp.Client { return s.otherClients }

func (s *stubLSPProvider) UnavailableReason(string) string { return s.reason }

func runDiagnostics(t *testing.T, provider LSPProvider, input string) ToolResponse {
	t.Helper()
	resp, err := NewDiagnosticsTool(provider).Run(context.Background(), ToolCall{Input: input})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return resp
}

func TestDiagnostics_ReportsInstallInstructions(t *testing.T) {
	provider := &stubLSPProvider{
		reason: "no language server available for .py files: pyright-langserver is not installed",
	}

	resp := runDiagnostics(t, provider, `{"file_path":"main.py"}`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got %+v", resp)
	}
	if !strings.Contains(resp.Content, "pyright-langserver is not installed") {
		t.Fatalf("content = %q", resp.Content)
	}
	if len(provider.ensured) != 1 || provider.ensured[0] != config.LSPTriggerExplicit {
		t.Fatalf("diagnostics must activate with the explicit trigger, got %v", provider.ensured)
	}
}

func TestDiagnostics_DoesNotFallBackToUnrelatedServers(t *testing.T) {
	// A Go server is running, but the caller asked about a Python file: its
	// diagnostics answer a different question and must not be reported.
	goClient := &lsp.Client{Languages: []string{".go"}}
	provider := &stubLSPProvider{
		reason:       "no language server available for .py files: pyright-langserver is not installed",
		otherClients: map[string]*lsp.Client{"gopls": goClient},
	}

	resp := runDiagnostics(t, provider, `{"file_path":"main.py"}`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got %+v", resp)
	}
	if strings.Contains(resp.Content, "gopls") {
		t.Fatalf("unrelated servers must not be queried, got %q", resp.Content)
	}
}

func TestDiagnostics_ProjectWideWithoutServers(t *testing.T) {
	resp := runDiagnostics(t, &stubLSPProvider{}, `{}`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got %+v", resp)
	}
	if !strings.Contains(resp.Content, "file_path") {
		t.Fatalf("the message must point at file_path, got %q", resp.Content)
	}
}
