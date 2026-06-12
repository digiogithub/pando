Change 1: stop announcing tools as `available_commands`
In `sendAvailableCommandsUpdate(...)`, `ListAvailableTools()` should not be used directly to populate ACP commands.

ACP commands should be their own list, something like:
- `goal`
- `goal-status`
- `goal-cancel`
- `compact`
- `summarize`

### Change 2: create a real ACP slash command registry
Instead of having only `parseSlashCommand(...)` hardcoded, there should be a table/registry of ACP commands with:
- name
- description
- usage
- handler

### Change 3: implement manual compaction in ACP
Leveraging the existing agent logic:
- `/compact`
- or `/summarize`

### Change 4: decide whether tools should be exposed as tools, not as commands
If the ACP client already knows how to render tools/tool calls, there's no need to sell them as slash commands.
