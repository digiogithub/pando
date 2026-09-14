package agui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/userinput"
)

const (
	// toolSearchToolName mirrors tools.toolSearchTool.Info().Name
	// (internal/llm/tools/tool_search.go): the unexported constant is not
	// exported from that package, so the literal is kept in sync here.
	toolSearchToolName = "tool_search"
	// mesnadaToolPrefix is the canonical name prefix every mesnada_* tool
	// registered by agent.CoderAgentToolsWithMesnada uses (spawn_agent,
	// list_tasks, wait_task, cancel_task, get_task/output, note, await, swarm).
	mesnadaToolPrefix = "mesnada_"
)

// agentPool owns the agent instances this adapter drives.
//
// This is the mechanism that keeps the integration non-invasive: instead of
// teaching the shared agent to accept per-run tools (which would mean changing
// agent.NewAgent / agent.Run and therefore every surface), the adapter builds
// its own instances with exactly the same exported constructors app.go uses,
// keyed by agent name plus the hash of the frontend toolset. Pages share a
// toolset, so in practice this holds one to three instances.
type agentPool struct {
	deps      Deps
	cfg       Config
	perms     permission.Service
	userInput userinput.Service
	pending   *pendingRegistry

	mu      sync.Mutex
	entries map[string]*poolEntry
}

type poolEntry struct {
	svc      agent.Service
	lastUsed time.Time
}

func newAgentPool(deps Deps, cfg Config, perms permission.Service, ui userinput.Service, pending *pendingRegistry) *agentPool {
	return &agentPool{
		deps:      deps,
		cfg:       cfg,
		perms:     perms,
		userInput: ui,
		pending:   pending,
		entries:   make(map[string]*poolEntry),
	}
}

// get returns the agent for the given name and frontend toolset, building it on
// first use. frontendTools is accepted now so P3 only has to add the proxies:
// the pool key already accounts for it, which means a page declaring different
// tools can never silently reuse another page's agent.
func (p *agentPool) get(name config.AgentName, frontendTools []Tool) (agent.Service, error) {
	key := poolKey(name, frontendTools)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.evictLocked()

	if e, ok := p.entries[key]; ok {
		e.lastUsed = time.Now()
		return e.svc, nil
	}

	svc, err := p.buildLocked(name, frontendTools)
	if err != nil {
		return nil, err
	}
	p.entries[key] = &poolEntry{svc: svc, lastUsed: time.Now()}
	return svc, nil
}

// buildLocked mirrors internal/app/app.go's coder-agent construction, with the
// adapter's own permission and user-input services substituted in.
func (p *agentPool) buildLocked(name config.AgentName, frontendTools []Tool) (agent.Service, error) {
	agentTools := p.buildToolsLocked(frontendTools)

	svc, err := agent.NewAgent(
		name,
		p.deps.Sessions,
		p.deps.Messages,
		agentTools,
		p.deps.Skills,
	)
	if err != nil {
		return nil, fmt.Errorf("agui: build agent %q: %w", name, err)
	}
	return svc, nil
}

// buildToolsLocked builds the tool set handed to agent.NewAgent: the coder
// tool set, adapter-wide filtering, HITL substitution, then frontend-tool
// proxies. Split out from buildLocked so tests can assert on the exact slice
// a run's tool schema is built from without needing a live model provider.
func (p *agentPool) buildToolsLocked(frontendTools []Tool) []tools.BaseTool {
	agentTools := agent.CoderAgentToolsWithMesnada(
		p.deps.Orchestrator,
		p.deps.Remembrances,
		p.deps.Gateway,
		p.perms,
		p.deps.History,
		p.deps.LSP,
		p.userInput,
		p.deps.Sessions,
	)

	// reserved captures every tool name Pando itself would have registered
	// for this agent, BEFORE the allow-list/Mesnada filter below removes any
	// of them. It is what the frontend-tool reserved-name guard uses further
	// down, so a client-declared frontend tool can never claim a name the
	// allow-list denies (e.g. "bash") just because that tool is no longer in
	// the filtered agentTools slice.
	reserved := make(map[string]bool, len(agentTools))
	for _, t := range agentTools {
		reserved[t.Info().Name] = true
	}

	// Adapter-wide Tools glob allow-list and Mesnada switch (config.AGUIConfig
	// .Tools / .Mesnada). This MUST run after agent.CoderAgentToolsWithMesnada,
	// which already applied agent.ApplyToolDiscovery internally: filtering the
	// slice it returns is what lets filterAGUITools also strip the tool_search
	// tool itself, closing the deferred-tool bypass its remote executor would
	// otherwise leave open onto the whole MCP catalog (see filterAGUITools).
	agentTools = filterAGUITools(agentTools, p.cfg.Tools, p.cfg.Mesnada)

	if p.cfg.HumanInTheLoop {
		// AskUserQuestion normally blocks on a local overlay nobody can see from
		// a browser. Substituting it — same Info(), different waiting room —
		// routes the question to the AG-UI client instead.
		for i, t := range agentTools {
			if t.Info().Name != tools.AskUserQuestionToolName {
				continue
			}
			agentTools[i] = &hitlQuestionTool{
				inner:   t,
				pending: p.pending,
				timeout: defaultFrontendToolTimeout,
			}
			break
		}
	}

	if p.cfg.FrontendTools && len(frontendTools) > 0 {
		agentTools = append(agentTools, newFrontendTools(frontendTools, reserved, p.pending)...)
	}

	return agentTools
}

// filterAGUITools applies the adapter-wide Tools glob allow-list and Mesnada
// switch to allTools, returning the kept subset in the same order.
//
// Unlike agent.filterToolsByNames (internal/llm/agent/tools.go), this is a
// plain subtractive filter: nothing is force-included. filterToolsByNames
// exists to keep a context-trimmed tool set usable (it always keeps bash,
// edit, view, glob, grep, write, patch, ls) — exactly the tools an
// adapter-wide allow-list needs to be able to exclude, so it must not be
// reused here.
//
// With an empty allow-list and Mesnada true (the defaults) it returns
// allTools unchanged, not a copy, so an adapter with no restriction
// configured produces a byte-identical tool set to before this filter
// existed.
func filterAGUITools(allTools []tools.BaseTool, allow []string, mesnada bool) []tools.BaseTool {
	if len(allow) == 0 && mesnada {
		return allTools
	}
	kept := make([]tools.BaseTool, 0, len(allTools))
	for _, t := range allTools {
		if aguiToolAllowed(t.Info().Name, allow, mesnada) {
			kept = append(kept, t)
		}
	}
	return kept
}

// aguiToolAllowed is the single predicate behind filterAGUITools.
func aguiToolAllowed(name string, allow []string, mesnada bool) bool {
	if !mesnada && strings.HasPrefix(name, mesnadaToolPrefix) {
		return false
	}
	if len(allow) == 0 {
		return true
	}
	// tool_search (internal/llm/tools/tool_search.go) is the single deferred
	// discovery+execution entry point agent.ApplyToolDiscovery wires up: its
	// remote executor can search and call any tool in the shared discovery
	// registry, including MCP catalog tools that were never even in allTools
	// as a direct entry. ToolDiscovery is visibility, not authorization, so
	// once an explicit allow-list is configured, tool_search is always
	// dropped too — never matched against allow, even by an explicit "*" or
	// literal "tool_search" glob — closing that bypass rather than trying to
	// scope its catalog/executor to the allowed set.
	if name == toolSearchToolName {
		return false
	}
	for _, pattern := range allow {
		if ok, err := path.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}

// evictLocked drops idle entries past the TTL and, when the pool is over its
// size cap, the least recently used ones.
func (p *agentPool) evictLocked() {
	now := time.Now()
	for key, e := range p.entries {
		if now.Sub(e.lastUsed) > p.cfg.AgentPoolTTL {
			delete(p.entries, key)
		}
	}
	if len(p.entries) < p.cfg.AgentPoolSize {
		return
	}
	type aged struct {
		key  string
		when time.Time
	}
	all := make([]aged, 0, len(p.entries))
	for key, e := range p.entries {
		all = append(all, aged{key: key, when: e.lastUsed})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].when.Before(all[j].when) })
	for i := 0; i <= len(all)-p.cfg.AgentPoolSize; i++ {
		delete(p.entries, all[i].key)
	}
}

// busy reports whether any pooled agent still has a run in flight. Used on
// shutdown diagnostics only.
func (p *agentPool) busy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.svc.IsBusy() {
			return true
		}
	}
	return false
}

// poolKey identifies an agent instance by name plus the schema of the frontend
// tools it must expose. Tool order is normalized so two clients declaring the
// same tools in a different order share one instance.
func poolKey(name config.AgentName, frontendTools []Tool) string {
	if len(frontendTools) == 0 {
		return string(name)
	}
	sorted := make([]Tool, len(frontendTools))
	copy(sorted, frontendTools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	h := sha256.New()
	for _, t := range sorted {
		h.Write([]byte(t.Name))
		h.Write([]byte{0})
		h.Write([]byte(t.Description))
		h.Write([]byte{0})
		if params, err := json.Marshal(t.Parameters); err == nil {
			h.Write(params)
		}
		h.Write([]byte{0})
	}
	return string(name) + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}
