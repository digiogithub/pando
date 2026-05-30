{{/* Claude 3.x — standard chat models (Haiku, Sonnet, Opus) */}}
# Provider Guidelines — Claude 3

## Tool Usage
- Parallelize independent tool calls in a single function_calls block.
- Read files before editing — never guess at file contents.
- Use search tools to understand the codebase before implementing changes.

## Output Style
- Do not add code explanation summaries after edits.
- Summarize tool output only when the user needs it in your response.
- Be concise and direct — keep responses focused on the task.

## Coding
- Make one logical change at a time.
- Keep changes minimal and focused on the task.
- Follow existing code conventions precisely.
- Use absolute file paths in all references.
- Fix problems at the root cause rather than applying surface-level patches.
- Do not add copyright or license headers unless specifically requested.
