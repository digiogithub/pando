package agui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/userinput"
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
		reserved := make(map[string]bool, len(agentTools))
		for _, t := range agentTools {
			reserved[t.Info().Name] = true
		}
		agentTools = append(agentTools, newFrontendTools(frontendTools, reserved, p.pending)...)
	}

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
