# VTCode Skills Architecture & Implementation

## Overview

VTCode implements the [open Agent Skills standard](http://agentskills.io/) with a sophisticated three-level loading system that optimizes context management and agent responsiveness. Skills are modular instruction sets that guide Claude on how to complete specific tasks.

## Three-Level Loading System

The progressive disclosure architecture minimizes context overhead while maintaining full functionality:

### Level 1: Metadata (~50 tokens)
- **Always loaded** in system prompt
- Contains: name, description, version, author, compatibility info
- **Minimal context overhead** — enabling skill discoverability without bloat
- Used for routing decisions and skill selection

### Level 2: Instructions (variable, <5K tokens typical)
- **Loaded on first trigger** when skill is actively used
- Contains: SKILL.md body with workflows, guidelines, and examples
- **Context-managed** with automatic LRU eviction
- Unloaded when session context approaches limits

### Level 3: Resources (on-demand, zero base cost)
- **Only loaded when requested** by the model
- Contains: scripts, templates, reference materials, examples
- **No context overhead** when unused
- Fetched explicitly via tool calls

## Directory Structure

### Traditional Skills (Anthropic Spec Compliant)
```
my-skill/
├── SKILL.md              # Metadata (YAML frontmatter) + Instructions (Markdown)
├── ADVANCED.md           # Optional: detailed guides and workflows
├── scripts/
│   └── helper.py         # Optional: executable scripts for the skill
└── templates/
    └── example.json      # Optional: reference materials and examples
```

### CLI Tool Skills
```
my-tool/
├── tool                  # Executable (any language — Python, Bash, compiled, etc.)
├── README.md             # Tool documentation and usage
├── tool.json             # Optional: configuration metadata
└── schema.json           # Optional: argument validation schema
```

## SKILL.md Format

```yaml
---
name: my-skill
description: Brief description of what this skill does and when to use it
version: 1.0.0
author: Your Name
license: MIT
compatibility: "claude-opus-4-6, claude-sonnet-4-6"
allowed-tools: "read, write, bash, edit"
user-invocable: true
when-to-use: "Use when the task requires [specific condition]"
when-not-to-use: "Avoid when [edge case], [condition]"
---

# My Skill

## Instructions
[Step-by-step guidance for Claude]

## Examples
- Example usage 1 with context
- Example usage 2 showing edge case

## Guidelines
- Guideline 1 for success
- Guideline 2 for robustness
```

## Metadata Fields

### Required
- `name` — Lowercase alphanumeric + hyphens, max 64 chars
  - Cannot contain "anthropic" or "claude"
  - Defaults to skill directory name if omitted
- `description` — Non-empty, max 1024 chars
  - Should include *what* it does and *when* to use it
  - Defaults to first paragraph of skill body if omitted

### Optional but Recommended
- `version` — Semantic versioning (e.g., "1.0.0")
- `author` — Skill creator name
- `license` — License name or reference
- `compatibility` — Product/model compatibility constraints
- `allowed-tools` — Space/comma-delimited allowed tools (e.g., "read, write, bash")
- `user-invocable` — Whether skill appears in user menus (default: true)
- `when-to-use` — Concrete triggers for automatic activation
- `when-not-to-use` — Negative examples and edge cases
- `disable-model-invocation` — Prevents model from triggering (user-only)
- `context` — Set to `fork` to run in isolated session context
- `agent` — Profile hint when `context = "fork"`

## Skill Discovery & Precedence

VTCode searches skills in this order (highest to lowest precedence):

1. **VT Code User Skills** — `~/.vtcode/skills/`
2. **VT Code Project Skills** — `.agents/skills/` (project-specific)
3. **Pi User Skills** — `~/.pi/skills/`
4. **Pi Project Skills** — `.pi/skills/`
5. **Claude User Skills** — `~/.claude/skills/` (nested discovery)
6. **Claude Project Skills** — `.claude/skills/` (nested discovery)
7. **Codex User Skills** — `~/.codex/skills/`

Skills from higher precedence locations **override** same-named skills from lower locations.
Claude directories support nested discovery: VTCode scans `**/*.claude/skills/**/SKILL.md`.

## Skill Types

### Traditional Skills
- Directory-based with SKILL.md following Anthropic spec
- Full integration with VTCode's prompt injection system
- Auto-discovered from filesystem
- Context-managed progressive loading

### CLI Tool Skills
- Any executable (Python, Bash, compiled binary, etc.)
- README.md documentation as skill instructions
- Automatically bridged to VTCode's tool registry
- Native execution with streaming output

### Hybrid Skills
- Combine instruction text with external tool execution
- Instructions provide context; tools provide capability
- Best for complex workflows (e.g., "kubernetes-deploy" skill using kubectl)

## Integration with Agent Execution

### Activation Flow
1. **Discovery**: VTCode scans all skill paths on startup and during `/skills list` commands
2. **Routing**: Metadata (description + `when-to-use` fields) used for automatic skill selection
3. **Injection**: Active skill metadata always in system prompt; instructions loaded on first trigger
4. **Execution**: Skills execute with full access to VTCode's unified tool registry (read, write, bash, edit, search)

### Context Management
- **Metadata**: Always present (~50 tokens/skill, negligible)
- **Instructions**: Loaded when first used; evicted via LRU when context > 80% capacity
- **Resources**: On-demand; not pre-loaded
- **Recall**: Model can request previously evicted skills; re-loaded from cache or filesystem

### Skill Invocation Modes

#### Implicit (Model-Driven)
- Model detects skill applicability from metadata
- Automatically integrates skill instructions into prompt
- Best for contextual, workflow-aware tasks

#### Explicit (User-Driven via Slash Command)
- `/skill <name> <task>` or `/skills list` then select
- Useful for deterministic production workflows
- Supports skill-specific argument hints

#### Programmatic (API-Driven)
- VTCode CLI: `vtcode skills info <name>`, `vtcode skills validate <path>`
- Full skill manifest inspection before execution

## Best Practices for Routing Quality

### Skill Metadata as Routing Rules
- **description**: What the skill does and the artifact/outcome it produces
- **when-to-use**: Concrete "use when" triggers (inputs, keywords, task shape)
- **when-not-to-use**: Explicit negative examples and edge cases

### Examples
```yaml
# ✓ Good: Specific routing triggers
when-to-use: "Use when task requires database schema analysis, when comparing field definitions across tables, or when debugging type conflicts"

when-not-to-use: "Avoid for ad-hoc SQL queries. Not suitable for real-time transaction analysis."

# ✗ Weak: Too generic
when-to-use: "Use for database work"
when-not-to-use: "Use for non-databases"
```

### Deterministic Production Workflows
For reliable triggering in production:
1. Explicitly name the skill in instructions: "Use skill: `database-schema-analyzer`"
2. Provide concrete trigger keywords in metadata
3. Disable model invocation (`disable-model-invocation: true`) for manual control if needed
4. Document edge cases extensively in `when-not-to-use`

## Skill Validation

```bash
# Validate a skill against Anthropic spec
vtcode skills validate ~/my-skill

# List all discovered skills with locations
vtcode skills list

# Show detailed skill info
vtcode skills info <name>

# Display skill search paths and structure
vtcode skills config
```

## Creating Custom Skills

### CLI Command
```bash
vtcode skills create ~/.vtcode/skills/my-skill
```

### Manual Process
1. Create directory: `my-skill/`
2. Write `SKILL.md` with metadata + instructions
3. Add optional `scripts/` with helper executables
4. Add optional `templates/` with reference materials
5. Validate: `vtcode skills validate ./my-skill`

### Example: Data Processing Skill
```yaml
---
name: data-transform
description: Transforms CSV/JSON data using Python pandas with validation and error recovery
version: 1.0.0
author: You
allowed-tools: "read, write, bash, edit"
when-to-use: "Use when transforming structured data (CSV→JSON, aggregation, filtering)"
---

# Data Transform Skill

## Instructions
1. Read source file and inspect structure
2. Write Python script using pandas
3. Execute with error handling
4. Validate output shape and contents

## Example
Transform sales.csv to JSON with aggregated totals by region.
```

## Architecture Highlights

### Skill Execution Engine
- **Tool Registry Integration**: Skills execute with access to VTCode's unified tools
- **Error Recovery**: Automatic re-read injection on edit failures
- **Output Streaming**: Live skill output to TUI with progress indication
- **Hook System**: Auto-detected build checks (cargo, npm, tsc) run after skill edits

### Context Optimization
- **Progressive Disclosure**: Metadata always available; instructions loaded on demand
- **Eviction Strategy**: LRU with hard floor (never drop system prompt or original task)
- **Recall System**: Full skill instructions cached in-process; re-fetch from disk if evicted

### Skill Versioning
- Semantic versioning (1.0.0 format)
- Backward compatibility checks via manifest inspection
- Tool requirement validation before execution

## Integration with VTCode's Prompt System

Skills are injected at **prompt assembly time** before every agent turn:

1. **System Prompt Construction**
   - Base instructions for Claude
   - Active skill metadata (name, description, version)
   - Available tools and their signatures

2. **Context Injection** (First Trigger)
   - Full skill instructions (SKILL.md body)
   - Examples and guidelines from skill metadata
   - Links to optional ADVANCED.md or scripts/

3. **State Carryover** (Long Sessions)
   - Previously executed skills remain in context
   - Evicted skills can be recalled via `recall` tool
   - Original task always preserved

## Comparison to Other Agents

| Feature | VTCode | Claude Code | OpenCode |
|---------|--------|-------------|----------|
| Skills standard | Anthropic spec | Proprietary | None |
| Progressive loading | ✓ (3 levels) | ✓ (2 levels) | ✗ |
| Context eviction | ✓ (LRU) | ✓ (reactive) | ✗ |
| CLI tool bridging | ✓ (native) | ✗ | ✗ |
| Nested discovery | ✓ | ✓ | ✗ |
| Deterministic routing | ✓ | ✓ | ✗ |
