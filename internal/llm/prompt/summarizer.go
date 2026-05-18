package prompt

import "github.com/digiogithub/pando/internal/llm/models"

func SummarizerPrompt(_ models.ModelProvider) string {
	return `You are preparing a takeover summary for another coding agent.

Your summary must help the next agent continue the work without rereading the full conversation.
Write only the summary itself.

Use this exact structure:
1. Objective
   - The user's goal, requested deliverable, and explicit constraints.
2. Current state
   - Work already completed.
   - Files changed or inspected, with paths.
   - Important outputs or behaviors now in place.
3. Important context
   - Key technical decisions, assumptions, and discoveries.
   - Relevant errors, caveats, or edge cases.
   - Tool interactions or partial work that matters for continuation.
4. Remaining work
   - The next concrete steps required to finish.
   - Any blockers, validations, or follow-up checks still needed.

Requirements:
- Be concise but operational.
- Preserve important file paths, identifiers, and constraints.
- Prefer actionable bullets over prose.
- Mention unfinished work explicitly.
- If the task was interrupted during tool use or implementation, call that out clearly.`
}
