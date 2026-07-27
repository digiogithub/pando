---
created_at: 2026-07-27T19:54:16.847586251Z
updated_at: 2026-07-27T19:54:16.847586251Z
---
# How to Create Pando Skills

## Overview

Pando skills are context-aware instruction sets that extend the agent's capabilities. They are discovered automatically from specific directories and loaded lazily (metadata always loaded, full instructions only when activated).

## File Structure

A skill is a directory containing a `SKILL.md` file with optional companion files:

```
.skill-name/
├── SKILL.md          # Required: metadata + instructions
├── REFERENCE.md      # Optional: detailed reference tables
└── evals/
    └── trigger-eval.json  # Optional: activation test cases
```

## SKILL.md Format

### YAML Frontmatter

```yaml
---
name: skill-name              # Unique identifier (default: directory name)
description: "..."            # One-line description (shown in metadata)
version: "1.0.0"              # Semver version
author: pando                 # Author name
license: MIT                  # License
compatibility: ">=0.1.0"      # Pando version compatibility
user-invocable: true          # Can be manually activated by user
when-to-use: keyword1, keyword2  # Comma-separated trigger words
when-not-to-use: keyword1    # Comma-separated exclusion words
disable-model-invocation: false  # If true, only user can activate
context: |                   # Optional: brief context for metadata
  Short context about this skill
---
```

### Markdown Body (Instructions)

After the frontmatter `---`, write the full instructions in Markdown. This is Level 2 content — loaded only when the skill is activated, consuming tokens from the context budget.

## Discovery Paths (Priority Order)

1. `~/.pando/skills/` — User global skills
2. `.pando/skills/` — Project-local skills (workDir)
3. `~/.claude/skills/` — Claude-compatible global skills
4. `.claude/skills/` — Claude-compatible project skills
5. Configured `extraPaths` — Additional paths from config

Skills from higher-precedence paths override same-named skills from lower paths.

## How Activation Works

### Metadata Level (Always Loaded)
~50 tokens per skill. The `name`, `description`, and `when-to-use` fields are always in the system prompt, allowing the model to know what skills exist.

### Instructions Level (Loaded on Demand)
When a skill matches (via `when-to-use` keyword matching in the prompt, or user manual activation), the full Markdown body is loaded into context.

### Token Management
The `ContextManager` tracks token usage and enforces a budget (default 80% threshold). When loading a new skill would exceed the budget, LRU eviction removes the least-recently-used skill's instructions.

## Keyword Matching (Router)

The `MatchSkillToPrompt` function checks if the user's prompt contains trigger words from `when-to-use`:
1. Normalizes both trigger and prompt (lowercase, alphanumeric only)
2. Checks if the full normalized trigger is a substring
3. Falls back to checking if all significant words (>2 chars, not stop words) are present

## Best Practices

1. **Keep `when-to-use` specific** — Use domain-specific keywords to avoid false activations
2. **Keep `description` concise** — It's always loaded, token cost matters
3. **Use `REFERENCE.md` for detail** — Large reference tables don't belong in the main instructions
4. **Include `when-not-to-use`** — Prevent activation for adjacent but unrelated topics
5. **Create eval tests** — Validate trigger accuracy with positive/negative cases
6. **Test token cost** — Large skills eat into context budget; split if needed

## Example: Minimal Skill

```markdown
---
name: my-skill
description: Brief description of what this skill does
when-to-use: keyword1, keyword2
---

# My Skill

## Instructions

Full instructions here...
```

## Example: Complete Skill (pando-doc)

See `.agents/skills/pando-doc/SKILL.md` for a complete example with:
- Multilingual documentation site maintenance
- Hugo CMS content generation workflow
- Git-based change tracking for blog posts
- Content templates and conventions
