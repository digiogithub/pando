# Plan: Session Cache & Tool Pagination Optimization for Pando

## Overview
Implement an in-memory session-scoped cache for tool responses that enables universal pagination across all tools, reducing token consumption by avoiding full content injection into context.

**Inspired by**: Claude Code's tool optimization patterns (temp file spillover, offset/limit pagination, output truncation).
**Key difference**: Pando uses in-memory session cache instead of disk-based temp files.

## Analysis Summary

### What Claude Code Does
| Mechanism | Implementation |
|-----------|---------------|
| Bash truncation | 30K chars, start+end halves with "[N lines truncated]" |
| View pagination | 2000 line default, offset/limit, 2000 char line truncation |
| Grep pagination | 100 results default, head_limit + offset params |
| Glob limiting | 100 files max, sorted by mtime |
| Large output spillover | >15K tokens → temp file on disk → reference in context |
| WebFetch cache | 15-minute auto-cleanup cache |

### What Pando Already Has
- Bash: 30K char truncation ✓
- View: 2000 line limit, offset/limit ✓
- Grep: 100 results, head_limit + offset ✓
- Glob: 100 files limit ✓
- Regex cache (session-scoped) ✓
- Response metadata with truncation flags ✓

### What Pando Needs (This Plan)
1. **Session cache in memory** (not temp files) for large responses
2. **Universal pagination** via cache_read tool
3. **Auto-interception** of large responses → cache + compact reference
4. **Enhanced pagination params** for bash, glob, fetch
5. **Prompt guidance** for LLM to use pagination efficiently
6. **MCP tool coverage** + monitoring

## Phases

### Phase 1: Session Tool Response Cache (Core Infrastructure)
**Fact ID**: `tools_cache_phase1_session_cache`
- Create `internal/llm/tools/cache.go` with SessionCache struct
- In-memory cache scoped to session lifetime
- LRU eviction, 50MB default limit
- Thread-safe with sync.RWMutex
- Wire lifecycle to session.Create/EndSession
- Context key for passing cache to tools

### Phase 2: Cache Read Tool + Universal Pagination
**Fact ID**: `tools_cache_phase2_cache_tool`
- Create `internal/llm/tools/cache_read.go`
- New `cache_read` tool with params: cache_id, offset, limit, pattern
- Pattern search within cached content
- Register in CoderAgentTools and TaskAgentTools
- Line-numbered output consistent with original

### Phase 3: Automatic Cache Interception Layer
**Fact ID**: `tools_cache_phase3_auto_caching`
- Create `internal/llm/tools/cache_interceptor.go`
- Threshold: >15K chars OR >300 lines → auto-cache
- Returns first page (200 lines) + cache_id reference
- Bypass list for small-response tools (edit, write, etc.)
- Wire into agent.go tool execution pipeline (line ~516)

### Phase 4: Enhanced Pagination Parameters for All Tools
**Fact ID**: `tools_cache_phase4_enhanced_pagination`
- Bash: add head_limit, tail_lines params
- Glob: add head_limit, offset params
- Fetch: add section, max_length params
- Standardized PaginationInfo struct in tools.go

### Phase 5: System Prompt & LLM Guidance Integration
**Fact ID**: `tools_cache_phase5_prompt_integration`
- Update tool descriptions with pagination guidance
- Add "Tool Output Optimization" section to system prompt
- Guide LLM to use cache_read for large outputs
- Encourage incremental reading over full dumps

### Phase 6: MCP Tool Integration & Monitoring
**Fact ID**: `tools_cache_phase6_mcp_integration`
- Apply cache interception to MCP tool responses
- Cache statistics tool for debugging
- TUI status bar with cache stats
- Lua hooks: hook_cache_store, hook_cache_evict, hook_cache_clear

## Architecture Diagram

```
┌─────────────────────────────────────────────────────┐
│                    Agent Loop                        │
│                                                      │
│  tool.Run() → ToolResponse                           │
│       │                                              │
│       ▼                                              │
│  ┌─────────────────────┐                             │
│  │  Cache Interceptor   │                            │
│  │  (Phase 3)           │                            │
│  │                      │                            │
│  │  < threshold? ───────┼──→ Pass through to LLM     │
│  │                      │                            │
│  │  ≥ threshold? ───────┼──→ Store in SessionCache   │
│  │                      │    Return first page +     │
│  │                      │    cache_id reference      │
│  └─────────────────────┘                             │
│                                                      │
│  LLM sees cache_id → calls cache_read tool           │
│       │                                              │
│       ▼                                              │
│  ┌─────────────────────┐                             │
│  │  SessionCache        │ (Phase 1)                  │
│  │  (in-memory, per     │                            │
│  │   session, LRU)      │                            │
│  │                      │                            │
│  │  GetPage(id,off,lim) │──→ Paginated response      │
│  │  Search(id,pattern)  │──→ Filtered results        │
│  └─────────────────────┘                             │
│       │                                              │
│       ▼ (on session end)                             │
│    cache.Clear()                                     │
└─────────────────────────────────────────────────────┘
```

## Priority & Dependencies
- Phase 1 → Phase 2 → Phase 3 (sequential, each builds on previous)
- Phase 4 (independent, can be done in parallel with Phase 2-3)
- Phase 5 (after Phase 3, needs cache system to reference)
- Phase 6 (after Phase 3, extends to MCP)

## Token Savings Estimate
- Typical large tool output: 5K-50K chars → reduced to 200 lines (~3K chars) + reference
- 80-95% token reduction for large outputs
- LLM reads only what it needs via pagination
- No disk I/O overhead (pure memory)

## Date Created: 2025-03-18
