package agent

import (
	"fmt"
	"strings"
)

const initialGoalPrompt = `# Goal Mode Instructions

You are in GOAL MODE. You have a persistent objective you must work toward autonomously until completion.

## Your Objective
%s

## How Goal Mode Works
1. Break the objective into concrete, actionable steps (use TodoWrite to track them).
2. Execute each step: plan, act (use tools), evaluate results.
3. After each action, evaluate: have I made progress? Am I stuck? Is the goal complete?
4. Continue iterating on steps until the objective is fully achieved.
5. When the goal is complete, you MUST end your response with "GOAL COMPLETED" and provide a summary of what was accomplished.
6. If the goal cannot proceed due to a blocker you cannot resolve, you MUST end your response with "GOAL BLOCKED: <description of what is needed to continue>".

## Rules for Goal Mode
- Work autonomously — do NOT ask the user for clarification during goal execution.
- Make reasonable assumptions and document them.
- After each step, explicitly state your progress evaluation.
- Use TodoWrite for ALL plans and step tracking. The agent should track its own progress.
- NEVER say "let me know if you want me to continue" or similar phrases — just keep working until the goal is achieved or genuinely blocked.
- Be efficient: if a task can be done in one tool call, do it in one call. Do NOT unnecessarily split tasks into multiple iterations.
- If you have done everything reasonable related to the goal, declare "GOAL COMPLETED" with a summary.

## Termination
- Explicit completion: Your response contains "GOAL COMPLETED" — the goal is done.
- Explicit blocking: Your response contains "GOAL BLOCKED: <reason>" — you need something external.
- Max iterations: You will be stopped externally if iterations exceed the limit.
- Stalling: If you make no progress for 3 consecutive iterations, you will be marked as stalled.
`

const goalContinuationPrompt = `# Goal Mode — Iteration %d of %d

Continuing to work toward the objective with the following context:

## Current Objective
%s

## Progress So Far
%s

## Next Step
%s

## Progress Evaluation (after completing this iteration, evaluate):
- Have I made measurable progress toward the objective?
- Are there remaining subtasks?
- Is the goal now complete?

If complete, end with "GOAL COMPLETED: <summary>".
If blocked, end with "GOAL BLOCKED: <what is needed>".
Otherwise, continue working.`

const goalSummaryPrompt = `Summarize what was accomplished toward the goal: "%s".

Iteration count: %d
Final status: %s
Progress log:
%s

If status is completed, provide a concise summary of all changes made.
If status is blocked or failed, describe what was accomplished and what remains.`

func BuildGoalInitialPrompt(objective string, additionalInstructions string) string {
	sections := []string{fmt.Sprintf(initialGoalPrompt, strings.TrimSpace(objective))}

	additionalInstructions = strings.TrimSpace(additionalInstructions)
	if additionalInstructions != "" {
		sections = append(sections, additionalInstructions)
	}

	return strings.Join(sections, "\n\n")
}

func BuildGoalContinuationPrompt(objective string, iteration int, maxIterations int, progress string, nextStep string) string {
	if iteration <= 0 {
		iteration = 1
	}
	if maxIterations < iteration {
		maxIterations = iteration
	}
	progress = strings.TrimSpace(progress)
	if progress == "" {
		progress = "No confirmed progress recorded yet."
	}
	nextStep = strings.TrimSpace(nextStep)
	if nextStep == "" {
		nextStep = "Take the next concrete action toward the objective and update the plan."
	}

	return fmt.Sprintf(goalContinuationPrompt, iteration, maxIterations, strings.TrimSpace(objective), progress, nextStep)
}
