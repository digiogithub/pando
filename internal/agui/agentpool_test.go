package agui

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mcpgateway"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// fakeAGUITool is a minimal tools.BaseTool stand-in for filter tests: only
// the name that filterAGUITools / aguiToolAllowed inspect matters here.
type fakeAGUITool struct{ name string }

func (f fakeAGUITool) Info() tools.ToolInfo { return tools.ToolInfo{Name: f.name} }
func (f fakeAGUITool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.ToolResponse{}, nil
}

func toolNameSet(in []tools.BaseTool) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, t := range in {
		out[t.Info().Name] = true
	}
	return out
}

// TestFilterAGUITools_EmptyAllowListIsNoOp is the PANDO-US-0011 acceptance
// criterion "an absent/empty Tools list leaves the toolset byte-identical to
// today": with no allow-list and Mesnada true (the resolved defaults),
// filterAGUITools must return the very same backing slice, not a copy with
// equal contents.
func TestFilterAGUITools_EmptyAllowListIsNoOp(t *testing.T) {
	input := []tools.BaseTool{fakeAGUITool{"bash"}, fakeAGUITool{"kb_search_documents"}}
	got := filterAGUITools(input, nil, nil, true)

	if len(got) != len(input) {
		t.Fatalf("expected %d tools unchanged, got %d", len(input), len(got))
	}
	// Mutate the input slice in place and confirm got observes it: proof got
	// shares the same backing array rather than being an equal-looking copy.
	input[0] = fakeAGUITool{"mutated"}
	if got[0].Info().Name != "mutated" {
		t.Fatal("filterAGUITools must return the input slice unchanged when there is nothing to filter")
	}
}

// TestFilterAGUITools_AllowListIsSubtractiveOnly is the PANDO-US-0011
// acceptance criterion for `[AGUI] Tools = ["gintrack__*", "kb_search_documents"]`:
// no bash/edit/write/patch/browser/mesnada_* survives, and nothing outside the
// glob is force-included (unlike agent.filterToolsByNames' alwaysIncludedTools).
func TestFilterAGUITools_AllowListIsSubtractiveOnly(t *testing.T) {
	input := []tools.BaseTool{
		fakeAGUITool{tools.BashToolName},
		fakeAGUITool{tools.EditToolName},
		fakeAGUITool{tools.WriteToolName},
		fakeAGUITool{tools.PatchToolName},
		fakeAGUITool{tools.BrowserNavigateToolName},
		fakeAGUITool{"mesnada_spawn_agent"},
		fakeAGUITool{"tool_search"},
		fakeAGUITool{"gintrack__search"},
		fakeAGUITool{"kb_search_documents"},
	}

	got := filterAGUITools(input, []string{"gintrack__*", "kb_search_documents"}, nil, true)
	names := toolNameSet(got)

	for _, denied := range []string{
		tools.BashToolName, tools.EditToolName, tools.WriteToolName, tools.PatchToolName,
		tools.BrowserNavigateToolName, "mesnada_spawn_agent", "tool_search",
	} {
		if names[denied] {
			t.Errorf("denied tool %q must not survive the allow-list filter", denied)
		}
	}
	for _, allowed := range []string{"gintrack__search", "kb_search_documents"} {
		if !names[allowed] {
			t.Errorf("allow-listed tool %q must survive the filter", allowed)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly the 2 allow-listed tools, got %d: %v", len(got), names)
	}
}

// TestAGUIToolAllowed_MesnadaSwitchWinsOverAllowList is the PANDO-US-0011
// acceptance criterion "[AGUI] Mesnada = false removes every mesnada_* tool
// with no Tools list present" -- and additionally proves Mesnada=false denies
// mesnada_* tools even when an allow-list would otherwise have matched them,
// since the two knobs are documented as independent.
func TestAGUIToolAllowed_MesnadaSwitchWinsOverAllowList(t *testing.T) {
	cases := []struct {
		name    string
		toolNam string
		allow   []string
		mesnada bool
		want    bool
	}{
		{"mesnada tool dropped, no allow-list, mesnada off", "mesnada_spawn_agent", nil, false, false},
		{"mesnada tool dropped even if the allow-list matches it", "mesnada_spawn_agent", []string{"mesnada_*"}, false, false},
		{"mesnada tool kept when the switch is on and no allow-list", "mesnada_spawn_agent", nil, true, true},
		{"non-mesnada tool unaffected by the switch", tools.BashToolName, nil, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aguiToolAllowed(tc.toolNam, tc.allow, nil, tc.mesnada); got != tc.want {
				t.Fatalf("aguiToolAllowed(%q, %v, %v) = %v, want %v", tc.toolNam, tc.allow, tc.mesnada, got, tc.want)
			}
		})
	}
}

// TestAGUIToolAllowed_DenyListWinsOverAllowList is the PANDO-US-0013
// acceptance criterion for a profile's DenyTools: a tool matching a
// DenyTools glob is dropped even when Tools would otherwise allow it, and an
// empty DenyTools leaves the allow-list's decision untouched.
func TestAGUIToolAllowed_DenyListWinsOverAllowList(t *testing.T) {
	cases := []struct {
		name string
		tool string
		allow,
		deny []string
		want bool
	}{
		{"deny wins over a matching allow entry", "bash", []string{"*"}, []string{"bash"}, false},
		{"deny glob wins over allow", tools.EditToolName, []string{tools.EditToolName}, []string{"edit*"}, false},
		{"no deny match leaves allow's decision", "kb_search_documents", []string{"kb_search_documents"}, []string{"bash"}, true},
		{"empty deny list changes nothing", "kb_search_documents", []string{"kb_search_documents"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aguiToolAllowed(tc.tool, tc.allow, tc.deny, true); got != tc.want {
				t.Fatalf("aguiToolAllowed(%q, allow=%v, deny=%v) = %v, want %v", tc.tool, tc.allow, tc.deny, got, tc.want)
			}
		})
	}
}

// TestFilterAGUITools_DenyOnlyStillFiltersUnderEmptyAllow guards the early-out
// in filterAGUITools: with Tools empty but DenyTools set, the fast "return
// allTools unchanged" path must not fire, or the deny-list would be silently
// ignored.
func TestFilterAGUITools_DenyOnlyStillFiltersUnderEmptyAllow(t *testing.T) {
	input := []tools.BaseTool{fakeAGUITool{"bash"}, fakeAGUITool{"kb_search_documents"}}
	got := filterAGUITools(input, nil, []string{"bash"}, true)
	names := toolNameSet(got)
	if names["bash"] {
		t.Fatal("bash must be dropped by DenyTools even with no Tools allow-list configured")
	}
	if !names["kb_search_documents"] {
		t.Fatal("kb_search_documents must survive: it matches no deny glob")
	}
}

// TestAGUIToolAllowed_ToolSearchAlwaysDeniedOnceAllowListSet locks down the
// load-bearing part of the story: once an explicit Tools allow-list is
// configured, tool_search is dropped unconditionally, even by a glob that
// would otherwise match it literally ("*" or "tool_search" itself). With no
// allow-list configured at all, tool_search is unaffected (today's
// behaviour, required for the byte-identical no-op).
func TestAGUIToolAllowed_ToolSearchAlwaysDeniedOnceAllowListSet(t *testing.T) {
	if !aguiToolAllowed("tool_search", nil, nil, true) {
		t.Fatal("tool_search must be unaffected when no Tools allow-list is configured")
	}
	for _, allow := range [][]string{{"*"}, {"tool_search"}, {"gintrack__*", "kb_search_documents"}} {
		if aguiToolAllowed("tool_search", allow, nil, true) {
			t.Fatalf("tool_search must be denied once an allow-list is set, allow=%v", allow)
		}
	}
}

// newAGUIDiscoveryTestGateway builds an in-memory MCP gateway with one
// catalog-only tool ("github_create_issue"), mirroring
// internal/llm/agent/tool_discovery_unified_test.go's helper: it is what lets
// tool_search's remote executor reach a tool that was never a direct entry in
// allTools, which is exactly the bypass this story closes.
func newAGUIDiscoveryTestGateway(t *testing.T) *mcpgateway.Gateway {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
CREATE TABLE IF NOT EXISTS mcp_tool_registry (
    id TEXT PRIMARY KEY,
    server_name TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    description TEXT,
    input_schema TEXT,
    last_discovered TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(server_name, tool_name)
);
CREATE TABLE IF NOT EXISTS mcp_tool_usage_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_id TEXT NOT NULL,
    session_id TEXT,
    called_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER,
    success BOOLEAN DEFAULT TRUE,
    FOREIGN KEY(tool_id) REFERENCES mcp_tool_registry(id) ON DELETE CASCADE
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"github/create_issue", "github", "create_issue",
		"Create a new issue in a GitHub repository", "{}", time.Now().UTC(),
	); err != nil {
		t.Fatalf("insert catalog row: %v", err)
	}

	return mcpgateway.NewGateway(db, mcpgateway.FavoriteConfig{
		Threshold: 5, MaxFavorites: 15, WindowDays: 30, DecayDays: 14,
	})
}

// TestFilterAGUITools_ToolSearchBypassRegression is the PANDO-US-0011
// regression test: it first proves the bypass is real (tool_search's search
// and execute surface reaches an MCP catalog tool that is not in the allowed
// set), then proves filterAGUITools closes it by dropping tool_search itself,
// leaving no way to reach that catalog tool through this agent's tool set at
// all.
func TestFilterAGUITools_ToolSearchBypassRegression(t *testing.T) {
	gw := newAGUIDiscoveryTestGateway(t)

	prev := config.Get()
	config.SetForTests(&config.Config{
		ToolDiscovery: config.ToolDiscoveryConfig{
			Enabled:        true,
			Mode:           "always",
			SearchLimit:    8,
			MaxDirectTools: 64,
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	agent.ResetSharedDiscoveryRegistry()
	t.Cleanup(agent.ResetSharedDiscoveryRegistry)

	live := []tools.BaseTool{
		fakeAGUITool{tools.BashToolName},
		fakeAGUITool{"kb_search_documents"},
	}
	visible := agent.ApplyToolDiscovery(live, gw)

	var search tools.BaseTool
	for _, v := range visible {
		if v.Info().Name == "tool_search" {
			search = v
		}
	}
	if search == nil {
		t.Fatal("expected tool_search in the unfiltered, post-ApplyToolDiscovery set")
	}

	// Sanity check: before the allow-list filter runs, tool_search really can
	// find the "github_create_issue" catalog tool -- a tool this test's
	// allow-list ("kb_search_documents" only) will deny. This is the bypass:
	// ToolDiscovery is visibility, not authorization.
	resp, err := search.Run(context.Background(), tools.ToolCall{
		Input: `{"query":"create github issue"}`,
	})
	if err != nil {
		t.Fatalf("search run: %v", err)
	}
	if resp.IsError || !strings.Contains(resp.Content, "github_create_issue") {
		t.Fatalf("expected the unfiltered tool_search to find github_create_issue, got: %s", resp.Content)
	}

	filtered := filterAGUITools(visible, []string{"kb_search_documents"}, nil, true)
	names := toolNameSet(filtered)

	if names["tool_search"] {
		t.Fatal("tool_search must be dropped once an explicit Tools allow-list is configured")
	}
	if names[tools.BashToolName] {
		t.Fatal("bash must be dropped by the allow-list")
	}
	if !names["kb_search_documents"] {
		t.Fatal("kb_search_documents must survive the allow-list")
	}
	// With tool_search gone from the set entirely, there is no remaining tool
	// this agent could call to reach github_create_issue: the deferred
	// executor bypass is closed, not merely hidden from search.
}

// newAGUIPoolTestConfig returns an agui.Config with the same resolved
// defaults agui.ConfigFromApp would produce for a config file that predates
// Tools/Mesnada: no restriction. Individual tests override Tools/Mesnada.
// AgentPoolSize/AgentPoolTTL are set to ConfigFromApp's own defaults (4,
// 30m): a zero value for either is not a shape ConfigFromApp ever produces
// (it always applies defaultPoolSize/defaultPoolTTL when the resolved value
// is <= 0/empty), and evictLocked assumes that invariant -- a zero
// AgentPoolTTL in particular makes it evict an entry the instant it is
// added, and an unset AgentPoolSize makes it index an empty slice.
func newAGUIPoolTestConfig() Config {
	return Config{
		Path:          defaultPath,
		Agents:        []config.AgentName{config.AgentCoder},
		FrontendTools: true,
		Mesnada:       true,
		AgentPoolSize: defaultPoolSize,
		AgentPoolTTL:  defaultPoolTTL,
	}
}

// newAGUITestPool builds an agentPool with no real collaborators (nil
// permission/user-input services, empty Deps): agent.CoderAgentToolsWithMesnada
// falls back to its no-gateway, no-orchestrator path, which only needs a
// non-nil global config (for the config.Get() calls it makes internally).
func newAGUITestPool(t *testing.T, cfg Config) *agentPool {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{})
	t.Cleanup(func() { config.SetForTests(prev) })

	return newAgentPool(Deps{}, cfg, nil, nil, newPendingRegistry())
}

// TestBuildToolsLocked_AllowListAndFrontendToolGuard exercises the real
// agentPool.buildToolsLocked wiring end to end (not a re-implementation of
// it): with a Tools allow-list configured, the coder tool set built from real
// tool constructors excludes bash/edit/write/patch/browser, and a frontend
// tool trying to claim the denied name "bash" is refused while an unrelated
// frontend tool name still registers on top of the allow-list -- the
// PANDO-US-0011 acceptance criteria for Tools filtering and the frontend-tool
// reserved-name guard.
func TestBuildToolsLocked_AllowListAndFrontendToolGuard(t *testing.T) {
	cfg := newAGUIPoolTestConfig()
	cfg.Tools = []string{"glob", "grep"}
	pool := newAGUITestPool(t, cfg)

	got := pool.buildToolsLocked(nil, []Tool{
		{Name: "bash"},      // denied: a real Pando tool name the allow-list excludes
		{Name: "showChart"}, // fine: not a Pando tool name at all
	})
	names := toolNameSet(got)

	for _, denied := range []string{
		tools.BashToolName, tools.EditToolName, tools.WriteToolName, tools.PatchToolName,
	} {
		if names[denied] {
			t.Errorf("denied tool %q must not be in the built tool set", denied)
		}
	}
	for _, allowed := range []string{tools.GlobToolName, tools.GrepToolName} {
		if !names[allowed] {
			t.Errorf("allow-listed tool %q must be in the built tool set", allowed)
		}
	}
	if !names["showChart"] {
		t.Fatal("a frontend tool with a non-conflicting name must still register on top of the allow-list")
	}
	// "bash" is refused as a frontend tool name: it is a real Pando tool name
	// the allow-list denies, so it must not reappear via the frontend proxy
	// even though it is absent from the filtered agentTools slice.
	if names[tools.BashToolName] {
		t.Fatal("a frontend tool must not be able to claim a name the allow-list denies")
	}
}

// TestBuildToolsLocked_MesnadaOffWithGatewayConfigStillDropsToolSearch is a
// lighter-weight companion to the bypass regression test above, confirming
// the same behaviour reached through the real buildToolsLocked path once
// ToolDiscovery is enabled process-wide: tool_search never reaches the built
// tool set when a Tools allow-list is configured.
func TestBuildToolsLocked_ToolSearchNeverReachesBuiltSetUnderAllowList(t *testing.T) {
	prev := config.Get()
	config.SetForTests(&config.Config{
		ToolDiscovery: config.ToolDiscoveryConfig{
			Enabled:        true,
			Mode:           "always",
			SearchLimit:    8,
			MaxDirectTools: 64,
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })
	agent.ResetSharedDiscoveryRegistry()
	t.Cleanup(agent.ResetSharedDiscoveryRegistry)

	cfg := newAGUIPoolTestConfig()
	cfg.Tools = []string{"glob", "grep"}
	pool := newAgentPool(Deps{}, cfg, nil, nil, newPendingRegistry())

	got := pool.buildToolsLocked(nil, nil)
	if toolNameSet(got)["tool_search"] {
		t.Fatal("tool_search must never reach the built tool set once a Tools allow-list is configured")
	}
}

// TestPoolGet_TwoProfilesOverSameBaseGetDistinctInstances is the PANDO-US-0013
// acceptance criterion: two profiles declared over the same Base agent get
// two distinct pooled agent.Service instances with different toolsets, and
// neither evicts the other on a pool lookup.
func TestPoolGet_TwoProfilesOverSameBaseGetDistinctInstances(t *testing.T) {
	cfg := newAGUIPoolTestConfig()
	pool := newAGUITestPool(t, cfg)

	backlog := Profile{Name: "backlog-assistant", Base: config.AgentCoder, Tools: []string{"gintrack__*"}, Mesnada: true}
	docs := Profile{Name: "docs-assistant", Base: config.AgentCoder, Tools: []string{"kb_search_documents"}, Mesnada: true}

	svcBacklog, err := pool.get(backlog.Name, backlog.Base, &backlog, nil)
	if err != nil {
		t.Fatalf("get(backlog): %v", err)
	}
	svcDocs, err := pool.get(docs.Name, docs.Base, &docs, nil)
	if err != nil {
		t.Fatalf("get(docs): %v", err)
	}

	if svcBacklog == svcDocs {
		t.Fatal("two profiles sharing a Base agent must get distinct agent.Service instances")
	}

	pool.mu.Lock()
	entries := len(pool.entries)
	pool.mu.Unlock()
	if entries != 2 {
		t.Fatalf("pool has %d entries, want 2 (one per profile, neither evicting the other)", entries)
	}

	// A repeat lookup of either profile must return the SAME instance, not a
	// fresh one that displaced the other.
	again, err := pool.get(backlog.Name, backlog.Base, &backlog, nil)
	if err != nil {
		t.Fatalf("get(backlog) again: %v", err)
	}
	if again != svcBacklog {
		t.Fatal("a repeat lookup of the same profile must reuse its pooled instance")
	}
	pool.mu.Lock()
	entries = len(pool.entries)
	pool.mu.Unlock()
	if entries != 2 {
		t.Fatalf("pool has %d entries after a repeat lookup, want still 2 (docs must not have been evicted)", entries)
	}
}

// TestPoolGet_ProfileToolAllowListEnforcedExactlyLikeAdapterWide is the
// PANDO-US-0013 acceptance criterion: a profile's tool allow-list is
// enforced on the run exactly as the adapter-wide list is, including the
// tool_search bypass regression.
func TestPoolGet_ProfileToolAllowListEnforcedExactlyLikeAdapterWide(t *testing.T) {
	prev := config.Get()
	config.SetForTests(&config.Config{
		ToolDiscovery: config.ToolDiscoveryConfig{
			Enabled: true, Mode: "always", SearchLimit: 8, MaxDirectTools: 64,
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })
	agent.ResetSharedDiscoveryRegistry()
	t.Cleanup(agent.ResetSharedDiscoveryRegistry)

	cfg := newAGUIPoolTestConfig()
	// Adapter-wide Tools is deliberately left unrestricted: the profile's own
	// (narrower) allow-list must still be what is enforced for its runs.
	pool := newAgentPool(Deps{}, cfg, nil, nil, newPendingRegistry())

	profile := &Profile{
		Name: "backlog-assistant", Base: config.AgentCoder,
		Tools: []string{tools.GrepToolName}, Mesnada: true,
	}
	got := pool.buildToolsLocked(profile, nil)
	names := toolNameSet(got)

	if names[tools.BashToolName] {
		t.Fatal("bash must be dropped by the profile's own allow-list")
	}
	if names["tool_search"] {
		t.Fatal("tool_search must be dropped once the profile's own allow-list is configured, exactly as the adapter-wide bypass regression requires")
	}
	if !names[tools.GrepToolName] {
		t.Fatal("grep must survive: it matches the profile's allow-list")
	}
}
