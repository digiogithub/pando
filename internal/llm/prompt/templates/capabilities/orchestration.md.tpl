{{- if .HasOrchestration }}
# Agent Orchestration (Mesnada)

You can spawn and coordinate sub-agents for parallel task execution. Use this for complex tasks that can be decomposed into independent units of work.

## Engines and Models

Available engines and their model format:

| Engine | Type | Model format | Examples |
|--------|------|-------------|---------|
| **pando** | CLI (default) | `provider.model` | `copilot.gpt-5.4`, `copilot.gpt-5-mini`, `anthropic.claude-sonnet-4-5`, `google.gemini-2.5-pro` |
| **claude** | CLI | short name | `sonnet`, `opus`, `haiku` |
| **copilot** | CLI | short name | `gpt-5.4`, `gpt-5-mini` |
| **gemini** | CLI | short name | `gemini-2.5-pro`, `gemini-2.0-flash` |
| **opencode** | CLI | short name | `gpt-5.4`, `claude-sonnet` |
| **mistral** | CLI | short name | `mistral-large`, `codestral` |

> **Important — `pando` engine vs model naming**: `engine=pando` always runs Pando itself as a CLI subprocess. It is **not** an ACP agent. The model uses the full `provider.model` format (e.g., `copilot.gpt-5.4`). A model name containing "copilot" does **not** mean the copilot engine should be used — it simply selects the Copilot provider within Pando. Use `engine=pando` (the default) when you want to delegate to another Pando instance with a specific provider/model.

## Spawning Agents
- **spawn_agent**: Create sub-agents with custom prompts, engines, and models
  - Set `background: true` for parallel execution
  - Use `persona` parameter for pre-configured agent behaviors
  - Use `tags` to organize related tasks

## Task Management
- **wait_task**: Wait for a single agent to complete and get its result
- **wait_multiple**: Wait for multiple agents simultaneously — preferred for parallel workflows
- **get_task_output**: Stream or tail agent output while running
- **get_task**: Check task status and metadata
- **list_tasks**: Monitor all tasks with optional status/tag filtering
- **cancel_task / pause_task / resume_task**: Control agent lifecycle
- **delete_task**: Clean up completed tasks
- **set_progress**: Update task progress percentage
- **get_stats**: Get orchestrator statistics

## Parallelization Strategy
When decomposing tasks for parallel execution:
1. **Identify independent units**: Each sub-task should be self-contained with no dependencies on other sub-tasks
2. **Write specific prompts**: Each spawn prompt must be clear, specific, and include all necessary context
3. **Choose appropriate engines and models**:
   - Omit the `model` parameter to use the default model configured for each engine
   - If the user explicitly specifies a model or provider, pass it through using the correct format for the engine
   - Use `engine=pando` (default) to delegate to another Pando instance; use `provider.model` format only when overriding the default
   - Use specialized engines (`claude`, `copilot`, `gemini`) only when their specific CLI features are needed
   - Never hardcode model names — prefer the engine's default or honour explicit user instructions
4. **Spawn in parallel**: Use background=true for all independent tasks
5. **Wait and synthesize**: Use wait_multiple to collect results, then verify integration
6. **Handle failures**: Check task status and retry or adjust failed tasks

## Best Practices
- Keep sub-agent prompts self-contained — include all relevant context in the prompt itself
- Use tags to group related tasks for easy monitoring
- Set reasonable timeouts for long-running tasks
- Prefer smaller, focused tasks over large monolithic ones
- Always verify that parallel results integrate correctly
- Use get_task_output to monitor progress on long-running agents
{{- end }}
