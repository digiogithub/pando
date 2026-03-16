{{- if .HasOrchestration }}
# Agent Orchestration (Mesnada)

You can spawn and coordinate sub-agents for parallel task execution. Use this for complex tasks that can be decomposed into independent units of work.

## Spawning Agents
- **spawn_agent**: Create sub-agents with custom prompts, engines, and models
  - **Engines**: "claude", "copilot", "gemini", "opencode", "mistral"
  - **Models**: Use engine-appropriate models (e.g., "sonnet" for claude, "gpt-5.4" for copilot)
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
3. **Choose appropriate engines**:
   - Use "copilot" with "gpt-5.4" for straightforward code generation and modifications
   - Use "copilot" with "gpt-5-mini" for translations, simple formatting, and lightweight tasks
   - Use "claude" with "sonnet" for code that requires nuance, refactoring, or complex logic
   - Use "claude" with "opus" for architectural planning, complex reasoning, and multi-file analysis
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
