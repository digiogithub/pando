package tools

import (
	"context"
	"strings"
	"testing"
)

// catalogLSPProvider is a stub provider that also answers the optional
// SetupLSPCatalog interface.
type catalogLSPProvider struct {
	stubLSPProvider
	entries []LSPCatalogEntry
}

func (c *catalogLSPProvider) LSPCatalog() []LSPCatalogEntry { return c.entries }

func testCatalog() []LSPCatalogEntry {
	return []LSPCatalogEntry{
		{
			Name: "gopls", Description: "Go language server", Command: "gopls",
			Languages: []string{".go"}, Configured: true,
			Availability: "installed", AvailabilityLabel: "installed", RunState: "ready",
		},
		{
			Name: "pyright", Description: "Python language server", Command: "pyright-langserver",
			Languages: []string{".py"}, Installing: true,
			Availability: "installable", AvailabilityLabel: "installable (bun)", RunState: "starting",
		},
		{
			Name: "rust-analyzer", Description: "Rust language server", Command: "rust-analyzer",
			Languages:    []string{".rs"},
			Availability: "manual", AvailabilityLabel: "not installed", RunState: "stopped",
			Hint: "rustup component add rust-analyzer", Reason: "rust-analyzer is not installed",
		},
		{
			Name: "biome", Description: "Biome linter", Command: "biome",
			Languages: []string{".ts"}, OptIn: true,
			Availability: "installable", AvailabilityLabel: "installable (bun)", RunState: "stopped",
		},
	}
}

func TestPandoSetupLSPDefaultView(t *testing.T) {
	tool := NewPandoSetupTool(nil, &catalogLSPProvider{entries: testCatalog()})

	resp := runSetupTool(t, tool, context.Background(), "lsp", "")
	if resp.IsError {
		t.Fatalf("lsp errored: %s", resp.Content)
	}
	for _, want := range []string{"gopls", "pyright", "on-demand activation"} {
		if !strings.Contains(resp.Content, want) {
			t.Fatalf("output omits %q:\n%s", want, resp.Content)
		}
	}
	// A stopped, uninstalled server is noise in the default view.
	if strings.Contains(resp.Content, "biome") {
		t.Fatalf("the default view must hide idle catalogue servers:\n%s", resp.Content)
	}
	if !strings.Contains(resp.Content, "more in the catalogue") {
		t.Fatalf("the default view must say how to see the rest:\n%s", resp.Content)
	}
}

func TestPandoSetupLSPAllAndMissing(t *testing.T) {
	tool := NewPandoSetupTool(nil, &catalogLSPProvider{entries: testCatalog()})

	all := runSetupTool(t, tool, context.Background(), "lsp", "--all")
	if !strings.Contains(all.Content, "biome") || !strings.Contains(all.Content, "opt-in") {
		t.Fatalf("--all must list opt-in servers:\n%s", all.Content)
	}

	missing := runSetupTool(t, tool, context.Background(), "lsp", "--missing")
	if !strings.Contains(missing.Content, "rustup component add rust-analyzer") {
		t.Fatalf("--missing must print the install command:\n%s", missing.Content)
	}
	if strings.Contains(missing.Content, "- gopls") {
		t.Fatalf("--missing must not list installed servers:\n%s", missing.Content)
	}
}

func TestPandoSetupLSPDetail(t *testing.T) {
	tool := NewPandoSetupTool(nil, &catalogLSPProvider{entries: testCatalog()})

	resp := runSetupTool(t, tool, context.Background(), "lsp", "rust-analyzer")
	if resp.IsError {
		t.Fatalf("lsp rust-analyzer errored: %s", resp.Content)
	}
	for _, want := range []string{"not installed", "rustup component add rust-analyzer", ".rs"} {
		if !strings.Contains(resp.Content, want) {
			t.Fatalf("detail omits %q:\n%s", want, resp.Content)
		}
	}

	unknown := runSetupTool(t, tool, context.Background(), "lsp", "nope")
	if !unknown.IsError {
		t.Fatalf("an unknown server must be an error, got: %s", unknown.Content)
	}
}

func TestPandoSetupLSPWithoutProvider(t *testing.T) {
	// The plain static provider does not implement SetupLSPCatalog.
	resp := runSetupTool(t, NewPandoSetupTool(nil, NewStaticLSPProvider(nil)), context.Background(), "lsp", "")
	if !resp.IsError {
		t.Fatalf("expected an error without a catalogue, got: %s", resp.Content)
	}

	resp = runSetupTool(t, NewPandoSetupTool(nil, nil), context.Background(), "lsp", "")
	if !resp.IsError {
		t.Fatalf("expected an error without a provider, got: %s", resp.Content)
	}
}
