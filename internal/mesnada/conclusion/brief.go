package conclusion

// BriefInstruction returns the instruction appended to a delegated subagent's
// prompt when delegation is enabled. It tells the subagent to close its run with
// a single <pando:conclusion> sentinel block listing ONLY the model-known fields.
// The software fills in all launch metadata (task id, engine, model, project,
// parent session, timestamps) from the task record it owns, so the model must NOT
// restate those.
//
// The text is centralized here so it is reusable and testable, and so the
// orchestrator can append it as the trailing instruction after persona is applied.
func BriefInstruction() string {
	return briefInstruction
}

const briefInstruction = `

---
IMPORTANT — Report your conclusion before you finish.

End your run with a SINGLE conclusion block in this exact form (the software
fills in all other metadata; do not restate task id, engine, model, or paths):

<pando:conclusion>
status: success        # one of: success | partial | failed | blocked
summary: |             # 2-4 sentences describing what you accomplished
  <concise summary of the outcome>
artifacts:             # files or kb doc-ids you produced (optional)
  - path/to/file
memory_refs:           # kb doc-ids / memory keys you wrote (optional)
  - <id>
follow_up: <what remains, or leave empty>
confidence: 0.0        # your confidence in the result, 0.0 to 1.0
</pando:conclusion>

Emit the block exactly once, as the very last thing in your output.`
