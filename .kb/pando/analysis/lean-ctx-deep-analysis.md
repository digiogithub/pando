---
created_at: 2026-06-25T22:00:30.676297381Z
updated_at: 2026-06-25T22:00:30.676297381Z
tags:
    - analysis
    - lean-ctx
    - token-optimization
    - reference
---
# Deep Analysis: lean-ctx (Context Intelligence Engine)

**Repository:** https://github.com/yvgude/lean-ctx
**Language:** Rust
**Description:** Context Intelligence Engine and Context OS for up to 99% LLM token reduction

## What is lean-ctx?

lean-ctx is a high-performance **Context Intelligence Engine** and **Context OS** that acts as an "Intelligence Buffer" between human developers and AI agents. It reduces LLM token consumption by up to 99% through multi-layered compression, AST-aware parsing, session management, and a science-grounded cognition system.

## Core Value Proposition

| Strategy | Implementation | Impact |
|----------|---------------|--------|
| Terse Engine | 4-layer compression pipeline | Adaptive information density |
| CCP | Context Continuity Protocol | -99% cold-start tokens |
| TDD | Token Dense Dialect (λ, §, ∂, τ) | 8-25% extra savings |
| Cognition v2 | Ebbinghaus forgetting, Hebbian eviction | Human-like context recall |

## Architecture: Four-Dimension Model

### Dimension 1 — Compression
- `Compression Engine` with 10+ read modes
- tree-sitter AST parsing for 18 languages
- Entropy-based filtering (Shannon entropy on BPE token IDs)
- Signature extraction (functions, classes, types)
- Terse Engine: 4-layer post-processing pipeline

### Dimension 2 — Routing
- `Semantic Router` selects optimal fidelity per read
- `ModePredictor` using Thompson Sampling bandits
- Auto mode resolves based on task, file size, history
- Adaptive compression thresholds

### Dimension 3 — Management
- `SessionCache` (MD5-based) — re-reads cost ~13 tokens
- `Property Graph` (SQLite) for relationship discovery
- `Context Continuity Protocol (CCP)` for cross-session memory
- `HandoffLedger` for multi-agent coordination

### Dimension 4 — Quality
- `PathJail` — path traversal prevention
- Secret defense (`.env` redaction)
- Formal verification via Lean4 proofs
- Budget enforcement (token, cost, disk, RAM limits)

## Compression Modes

| Mode | Strategy | Description |
|------|----------|-------------|
| Full | `format_full_output` | Entire file with metadata |
| Signatures | AST extraction | API surface only (functions/classes/types) |
| Map | `structured_read` | Structural overview for large files |
| Aggressive | Remove comments/blanks | Normalizes indentation |
| Entropy | Shannon entropy | Drops low-information lines |
| Lines | Line-range window | Specific line ranges |
| Diff | Compact unified diff | Changes since last read |
| Auto | Smart selection | Based on task/size/history |

## Core Modules

| Domain | Key Modules | Responsibility |
|--------|------------|----------------|
| Compression | compressor, entropy, information_bottleneck | Multi-mode file compression |
| Memory | episodic_memory, memory_lifecycle, memory_policy | Long-term fact retention, Ebbinghaus eviction |
| Graph | property_graph, graph_index, call_graph | AST-based dependency indexing |
| Context | context_os, context_compiler, context_proof | Shared sessions, formal verification |
| Knowledge | cognition_loop, claim_extractor, knowledge | Observation synthesis |
| Search | hybrid_search, bm25_index, semantic_cache | Multi-vector retrieval |
| Session | session, ccp_session_bundle, handoff_ledger | CCP state persistence |

## Integration Methods

### 1. Shell Hook
Transparently wraps CLI tools (95+ tools):
```bash
git status → lean-ctx -c git status
```
24 default aliases installed via `lean-ctx setup`.

### 2. MCP Server
77+ specialized tools for MCP-compatible editors:
- `ctx_read` — cached file reads with compression
- `ctx_graph` — relationship discovery
- `ctx_session` — session management
- `ctx_knowledge` — knowledge extraction
- `ctx_compress` — on-demand compression
- `ctx_preload` — task-relevant file preloading

### 3. AI Tool Hooks
One-command integration for 20+ agents:
- Cursor, Claude Code, Windsurf, Cline, Zed, JetBrains
- Auto-detection of agent config paths

## Configuration System

5-layer resolution order:
1. Environment Variables (highest)
2. Project-Local Config (`.lean-ctx.toml`)
3. Active Profile
4. Global Config (`~/.lean-ctx/config.toml`)
5. Built-in Defaults (lowest)

Unified `CompressionLevel` enum: off → lite → standard → max

## Advanced Features

### Context Continuity Protocol (CCP)
- Persists `SessionState` (task, findings, decisions)
- Cold-start resumes with ~400 tokens instead of re-reading project
- Cross-session memory with handoff ledgers

### Token Dense Dialect (TDD)
Mathematical symbol shorthand:
- λ = Function/Method
- § = Struct/Class
- ∂ = Interface/Trait
- τ = Type Alias
- ε = Enum
8-25% additional savings

### Property Graph (SQLite-backed)
- Tracks imports, calls, exports
- Weighted impact analysis
- Related file discovery
- Call graph analysis

### Cognition Loop
9-step pipeline for observation synthesis:
- Claim extraction
- Knowledge synthesis
- Ebbinghaus forgetting model
- Hebbian eviction
- Memory lifecycle management

### LITM-Aware Positioning
- Addresses "Lost-in-the-Middle" phenomenon
- Priority tiers (P1/P2/P3)
- Block anchoring with delimiters
- Attention-aware layout

### Formal Verification (Lean4)
Proven correctness for:
- PathJail (path isolation)
- BudgetEnforcement (cost limits)
- SecretSafety (sensitive pattern redaction)
- TerseQuality (semantic equivalence)
- ReadModes (instruction file preservation)

## Task Relevance and Preloading

- Heat diffusion + PageRank centrality on project graph
- Boltzmann distribution for token budget allocation
- `ctx_preload` tool for proactive context loading

## Strengths

1. **AST-aware compression** — tree-sitter parsing for 18 languages
2. **Session continuity** — CCP with cross-session memory
3. **77+ MCP tools** — comprehensive programmatic API
4. **Adaptive routing** — Thompson Sampling for mode selection
5. **Formal verification** — Lean4 proofs for safety properties
6. **Property graph** — deep code intelligence
7. **Entropy filtering** — information-theoretic compression
8. **LITM optimization** — attention-aware context positioning
9. **Cognition loop** — science-grounded memory management
10. **Budget enforcement** — token, cost, disk, RAM limits

## Weaknesses

1. **Higher complexity** — more moving parts than simpler alternatives
2. **Rust-only core** — no Go/Python SDK for embedding
3. **Heavy dependency tree** — tree-sitter, SQLite, axum, etc.
4. **Experimental features** — some cognition features are v2/experimental
5. **No direct CI/CD integration** — focused on developer workflow, not pipelines
6. **Token estimation** — still relies on heuristic (chars/4) for basic counting
7. **Configuration surface** — many config options can overwhelm users
