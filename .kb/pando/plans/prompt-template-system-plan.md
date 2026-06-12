# Implementation Plan: Prompt Template System for Pando

## Executive Summary

Complete redesign of Pando's system prompt generation, migrating from hardcoded prompts in Go constants to a modular template system (.md.tpl) with layered composition, conditional sections based on available capabilities, LLM provider optimization, and full hookability via Lua scripts.

## Reference Analysis

### Analyzed sources:
1. **Crush** (Go) — Template-based with PromptDat struct, coder.md.tpl/task.md.tpl, MCP instructions injection, skills XML, context files loop. Lazy async building.
2. **OpenCode** (TypeScript) — Layered composition (provider → environment → skills → instructions), provider-specific prompts per model family, multiple modes (build/plan/explore/compaction), plugin hooks for transformation.
3. **Claude Code** — Hierarchical composition (Identity → Responsibilities → Process → Quality → Output → Edge Cases), frontmatter metadata, example-driven triggering, hook event types for lifecycle, agent archetypes.
4. **Current Pando** — Multi-agent (coder/task/title/summarizer), provider-specific (Anthropic vs OpenAI), Lua hooks (6 types), skills injection, MCP tool filtering, persona system for sub-agents.

### Best selected characteristics:
- **From Crush**: Separate .md.tpl files, PromptDat struct, async building, Go template engine
- **From OpenCode**: Layered composition, provider-specific prompts, multiple modes (plan/explore), capability awareness
- **From Claude Code**: Hierarchical section structure, edge case handling, quality standards, prompt validation
- **From Pando**: Existing Lua system, lifecycle hooks, MCP filtering, persona system

## Final Architecture

```
internal/llm/prompt/
├── templates/                      # Embedded templates (embed.FS)
│   ├── base/                       # Reusable sections
│   │   ├── identity.md.tpl
│   │   ├── environment.md.tpl
│   │   ├── conventions.md.tpl
│   │   ├── workflow.md.tpl
│   │   ├── tone.md.tpl
│   │   └── tools_policy.md.tpl
│   ├── agents/                     # Template per agent type
│   │   ├── coder.md.tpl
│   │   ├── task.md.tpl
│   │   ├── planner.md.tpl         # NEW
│   │   ├── explorer.md.tpl        # NEW
│   │   ├── title.md.tpl
│   │   └── summarizer.md.tpl
│   ├── providers/                  # Provider optimizations
│   │   ├── anthropic.md.tpl
│   │   ├── openai.md.tpl
│   │   ├── gemini.md.tpl
│   │   └── ollama.md.tpl
│   ├── capabilities/               # Conditional sections
│   │   ├── remembrances.md.tpl
│   │   ├── orchestration.md.tpl
│   │   ├── web_search.md.tpl
│   │   ├── code_indexing.md.tpl
│   │   └── lsp.md.tpl
│   └── context/                    # Dynamic context
│       ├── git.md.tpl
│       ├── project.md.tpl
│       ├── skills.md.tpl
│       └── mcp_instructions.md.tpl
├── data.go                         # PromptData struct
├── registry.go                     # Template loading, caching, override
├── builder.go                      # Composition pipeline + CapabilityDetector
├── prompt.go                       # Entry point (refactored)
├── builder_test.go                 # Unit tests
└── integration_test.go             # Integration tests with Lua

# New Lua hooks:
luaengine/types.go  →  HookTemplateSection, HookCapabilityCheck, HookProviderSelect, HookPromptCompose
luaengine/functions.go → pando_get_config, pando_get_git_status, pando_list_mcp_servers, pando_list_tools, pando_render_template, pando_load_file
```

## Composition Pipeline

```
1. Load identity template (base/identity.md.tpl)
2. Select provider template → hook_provider_select → render
3. Select agent template (agents/{name}.md.tpl) → render
4. Render environment section (base/environment.md.tpl)
5. Detect capabilities → hook_capability_check per capability → render matching templates
6. Render context sections (git, project files, skills, MCP instructions)
7. Apply hook_template_section to EACH section
8. Apply hook_prompt_compose to reorder/add/remove sections
9. Join all sections
10. Apply hook_system_prompt (backward compatible)
11. Return final prompt
```

## Implementation Phases

### Phase 1: Template Infrastructure and PromptBuilder
**Fact ID**: prompt_system_plan_phase1
**Scope**: PromptData struct, TemplateRegistry, PromptBuilder, refactoring of GetAgentPrompt
**Dependencies**: None
**Priority**: CRITICAL — foundation for everything else

### Phase 2: Agent Templates and Base Sections
**Fact ID**: prompt_system_plan_phase2
**Scope**: Migration of hardcoded prompts to .md.tpl, new planner/explorer agents
**Dependencies**: Phase 1
**Priority**: HIGH — core functionality

### Phase 3: Provider Templates
**Fact ID**: prompt_system_plan_phase3
**Scope**: Specific templates for Anthropic, OpenAI, Gemini, Ollama
**Dependencies**: Phase 2
**Priority**: MEDIUM — optimization

### Phase 4: Conditional Capability Templates
**Fact ID**: prompt_system_plan_phase4
**Scope**: CapabilityDetector, templates for remembrances/mesnada/web/code/lsp
**Dependencies**: Phase 1
**Priority**: HIGH — key Pando differentiator

### Phase 5: Advanced Lua Hooks Integration
**Fact ID**: prompt_system_plan_phase5
**Scope**: New hooks (template_section, capability_check, prompt_compose, provider_select), pando_* functions
**Dependencies**: Phases 1, 4
**Priority**: MEDIUM — extensibility

### Phase 6: Testing, Documentation and Context Templates
**Fact ID**: prompt_system_plan_phase6
**Scope**: Context templates, test suite, documentation, examples
**Dependencies**: Phases 1-5
**Priority**: HIGH — quality and maintainability

## Recommended Execution Order

```
Phase 1 ─────┬──→ Phase 2 ──→ Phase 3
              │
              └──→ Phase 4 ──→ Phase 5
                                 │
                    Phase 6 ←────┘
```

Phases 2-3 and 4-5 can run in parallel after completing Phase 1.

## Design Principles

1. **Backward Compatible**: Generated prompts must be equivalent to current ones until new features are opted into
2. **Opt-in Complexity**: Capabilities, providers and hooks are only activated when available
3. **Override Pattern**: External templates override embedded ones; Lua hooks can modify any section
4. **Minimal Token Usage**: Ollama templates significantly shorter; capabilities only included if active
5. **Testable**: Each component with unit tests; integration tests for the complete chain
6. **Composable**: Independent sections that compose in pipeline

## Plan date: 2026-03-16
## Status: APPROVED PENDING IMPLEMENTATION