---
created_at: 2026-07-17T11:52:34.990218514Z
updated_at: 2026-07-17T11:52:34.990218514Z
tags:
    - documentation
    - slash-commands
    - pando-docs
---
# Slash Commands Documentation Update

## Date
2026-07-17

## What Changed
Updated the slash commands documentation in pando-docs for both English and Spanish versions.

## Files Modified
- `/www/MCP/Pando/pando-docs/content/en/docs/features/slash-commands.md`
- `/www/MCP/Pando/pando-docs/content/es/docs/features/slash-commands.md`

## Documentation Coverage
The documentation now covers all slash commands available in Pando:

### Goal Mode Commands
- `/goal <objective>` - Start autonomous goal mode
- `/autopilot <objective>` - Alias for /goal
- `/goal-status` - Show current goal status
- `/goal-cancel` - Cancel running goal

### Session Management Commands
- `/compact` - Create manual compact summary
- `/summarize` - Alias for /compact
- `/db-compact` - Compact database (SQLite VACUUM)

### Code Quality Commands
- `/ponytail [lite|full|ultra|off]` - Toggle YAGNI mode
- `/caveman [lite|full|ultra|wenyan]` - Toggle output brevity
- `/caveman-finish` - Disable caveman mode

### Workflow Commands
- `/superpowers [objective]` - Enable disciplined development workflow
- `/superpowers-finish` - Verify and return to normal mode
- `/learning [focus]` - Enable learner/documentarian mode
- `/learning-finish` - Consolidate learnings and return to normal

### Project Management Commands
- `/improve-agents-md [guidance]` - Create/reinforce AGENTS.md

### Custom Commands
- Project commands: `<data-dir>/commands/` (prefix: `project:`)
- User commands: `~/.config/pando/commands/` (prefix: `user:`)

## Interfaces Covered
- TUI: Fuzzy-searchable dialog
- Web UI: Autocomplete dropdown
- ACP: Editor integration (VS Code, Zed, JetBrains)
- CLI: Flags instead of slash commands

## Verification
- Hugo build successful: 106 EN pages, 105 ES pages
- No build errors

## Source Code References
- `internal/commands/registry.go` - Shared command registry
- `internal/completions/slash_commands.go` - TUI/WebUI completion provider
- `internal/mesnada/acp/slash_commands.go` - ACP command definitions
- `internal/superpowers/superpowers.go` - Superpowers mode implementation
- `internal/caveman/caveman.go` - Caveman mode implementation
- `internal/learning/learning.go` - Learning mode implementation
