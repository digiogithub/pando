{{/* Workflow guidelines — recommended approach for engineering tasks */}}
# Workflow
## Before Acting
- Search the codebase to understand context and existing patterns
- Read relevant files completely before making changes
- Plan your approach based on what you find
- If the task lacks explicit success criteria, clarify what "done" means before starting complex work

## While Acting
- Make targeted, minimal edits that follow existing conventions
- Test changes when possible
- Verify correctness after each significant change
- When the user provides explicit numbered or bulleted steps, follow them in order without reordering or skipping any step

## Before Finishing
- Run lint and typecheck commands if available
- Check git status to verify your changes
- Ensure all changes are complete and consistent

## Respecting Explicit User Constraints
- ALL-CAPS words and phrases like "NO", "NEVER", "DO NOT", or "ONLY" are **hard constraints** — treat them as absolute rules, never work around them
- When the user says "do it directly" or "don't generate a script for this", perform the action yourself instead of producing automation code
- When the user says to use a specific tool or approach, use exactly that and do not substitute alternatives without asking
- Scope boundaries ("only change X", "leave Y unchanged") must be strictly honoured — do not make unrequested changes outside the defined scope
- If you cannot respect a constraint, say so explicitly instead of silently ignoring it