package tooldiscovery_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// capturingTool records the last ToolCall it received.
type capturingTool struct {
	stubTool
	lastInput string
}

func (c *capturingTool) Run(_ context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	c.lastInput = params.Input
	return tools.NewTextResponse("ok-" + c.name), nil
}

func TestRegistry_UpsertReplacesExisting(t *testing.T) {
	reg := tooldiscovery.NewRegistry()

	first := &stubTool{name: "bash", desc: "v1"}
	second := &stubTool{name: "bash", desc: "v2"}

	if err := reg.Register(first, tooldiscovery.SourceCore, true); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := reg.Register(second, tooldiscovery.SourceCore, true); err != nil {
		t.Fatalf("register second: %v", err)
	}

	if got := len(reg.All()); got != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", got)
	}
	if reg.Resolve("bash") != tools.BaseTool(second) {
		t.Error("upsert must keep the most recent tool instance")
	}
}

func TestRegistry_SyncCatalogEntries(t *testing.T) {
	reg := tooldiscovery.NewRegistry()

	// A live tool shadowing one catalog name (e.g. a gateway favorite).
	live := &stubTool{name: "srv_foo", desc: "favorite"}
	if err := reg.Register(live, tooldiscovery.SourceMCP, false); err != nil {
		t.Fatalf("register live: %v", err)
	}

	metas := []tooldiscovery.ToolMetadata{
		{CanonicalName: "srv_foo", ServerName: "srv", ToolName: "foo", Source: tooldiscovery.SourceMCP, Description: "catalog foo"},
		{CanonicalName: "srv_bar", ServerName: "srv", ToolName: "bar", Source: tooldiscovery.SourceMCP, Description: "catalog bar"},
	}
	reg.SyncCatalogEntries(metas)

	// Live tool must win over the catalog entry.
	if reg.Resolve("srv_foo") != tools.BaseTool(live) {
		t.Error("live tool must shadow the catalog entry")
	}
	// srv_bar must exist as catalog-only.
	entry, ok := reg.GetEntry("srv_bar")
	if !ok {
		t.Fatal("srv_bar catalog entry missing")
	}
	if entry.Tool != nil {
		t.Error("srv_bar must be catalog-only (Tool == nil)")
	}
	if !reg.HasRemoteEntries() {
		t.Error("registry must report remote entries")
	}

	// Re-sync without srv_bar removes it; discovered state is unaffected.
	reg.MarkDiscovered("srv_foo")
	reg.SyncCatalogEntries([]tooldiscovery.ToolMetadata{
		{CanonicalName: "srv_foo", ServerName: "srv", ToolName: "foo", Source: tooldiscovery.SourceMCP},
	})
	if _, ok := reg.GetEntry("srv_bar"); ok {
		t.Error("srv_bar must disappear after catalog re-sync")
	}
	if !reg.IsDiscovered("srv_foo") {
		t.Error("discovered state must survive catalog re-sync")
	}
}

func TestRegistry_ExecuteTool_Local(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	tool := &capturingTool{stubTool: stubTool{name: "local_tool", desc: "local"}}
	if err := reg.Register(tool, tooldiscovery.SourceInternal, false); err != nil {
		t.Fatalf("register: %v", err)
	}

	resp, err := reg.ExecuteTool(context.Background(), "local_tool", map[string]interface{}{"a": 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.IsError || !strings.Contains(resp.Content, "ok-local_tool") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(tool.lastInput), &got); err != nil {
		t.Fatalf("input must be JSON-marshalled params: %v", err)
	}
	if got["a"] != float64(1) {
		t.Errorf("expected a=1 in input, got %v", got)
	}
	if !reg.IsDiscovered("local_tool") {
		t.Error("executed tool must be marked discovered")
	}
}

func TestRegistry_ExecuteTool_Remote(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	if err := reg.RegisterCatalogEntry(tooldiscovery.ToolMetadata{
		CanonicalName: "srv_remote",
		ServerName:    "srv",
		ToolName:      "remote",
		Source:        tooldiscovery.SourceMCP,
		Description:   "remote only",
	}); err != nil {
		t.Fatalf("register catalog: %v", err)
	}

	var gotServer, gotTool string
	var gotParams map[string]interface{}
	reg.SetRemoteExecutor(func(_ context.Context, server, tool string, params map[string]interface{}) (string, error) {
		gotServer, gotTool, gotParams = server, tool, params
		return "remote-result", nil
	})

	resp, err := reg.ExecuteTool(context.Background(), "srv_remote", map[string]interface{}{"q": "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.IsError || resp.Content != "remote-result" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if gotServer != "srv" || gotTool != "remote" || gotParams["q"] != "x" {
		t.Errorf("executor got server=%q tool=%q params=%v", gotServer, gotTool, gotParams)
	}
}

func TestRegistry_ExecuteTool_NotFound(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	resp, err := reg.ExecuteTool(context.Background(), "missing", nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.IsError {
		t.Error("expected error response for unknown tool")
	}
}

func TestSearch_FindsCatalogOnlyEntries(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	if err := reg.RegisterCatalogEntry(tooldiscovery.ToolMetadata{
		CanonicalName: "github_create_issue",
		ServerName:    "github",
		ToolName:      "create_issue",
		Source:        tooldiscovery.SourceMCP,
		Description:   "Create a new issue in a GitHub repository",
	}); err != nil {
		t.Fatalf("register catalog: %v", err)
	}

	results := reg.Search("create github issue", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].CanonicalName != "github_create_issue" {
		t.Errorf("unexpected result: %+v", results[0])
	}
	if results[0].Description == "" {
		t.Error("catalog-only entries must expose their metadata description")
	}
}

func TestPolicy_RemoteEntriesForceToolSearchBelowThreshold(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	// A small live set, below the activation threshold.
	for _, name := range []string{"bash", "edit"} {
		_ = reg.Register(&stubTool{name: name, desc: "core"}, tooldiscovery.SourceCore, true)
	}
	// One catalog-only MCP entry: only reachable through tool_search.
	_ = reg.RegisterCatalogEntry(tooldiscovery.ToolMetadata{
		CanonicalName: "srv_remote",
		ServerName:    "srv",
		ToolName:      "remote",
		Source:        tooldiscovery.SourceMCP,
	})

	cfg := tooldiscovery.DefaultPolicyConfig()
	policy := tooldiscovery.NewSelectionPolicy(cfg)
	searchTool := &stubTool{name: "tool_search", desc: "search"}

	visible := policy.Apply(reg, searchTool)
	names := map[string]bool{}
	for _, v := range visible {
		names[v.Info().Name] = true
	}
	if !names["tool_search"] {
		t.Error("tool_search must be visible whenever remote catalog entries exist")
	}
	if !names["bash"] || !names["edit"] {
		t.Error("live tools must stay visible below threshold")
	}
	if names["srv_remote"] {
		t.Error("catalog-only entries cannot appear as direct tools")
	}
}
