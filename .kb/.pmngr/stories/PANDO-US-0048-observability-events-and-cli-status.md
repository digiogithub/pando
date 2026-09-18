---
id: PANDO-US-0048
type: story
title: Observability, events and CLI status
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 3
created: 2026-09-18T08:36:28Z
updated: 2026-09-18T08:36:28Z
---

## Description

**As a** user or operator **I want** to see why and when the sandbox acted **so that** I can debug failures and audit escalations.

### Implementation

- `internal/sandbox/events.go`: slog Info `sandbox.applied`, `sandbox.unavailable` (once per process), `sandbox.denied`, `sandbox.escalation.{requested,granted,denied}` with `session_id` and backend. Commands are redacted through `internal/redact`.
- Ring buffer plus `.pando/data/sandbox-events.jsonl`, written by Pando only.
- Counters are exposed through `pando_stats`/`internal/stats`, and the events are forwarded to the existing Better Stack telemetry when opt-in is enabled.
- CLI `pando sandbox status` (capabilities, effective policy, protected paths) and `pando sandbox exec -- <cmd>` for manual testing.
- Include the sandbox block in `/doctor`-style diagnostics if such a command exists.

## Acceptance Criteria

- [ ] `pando sandbox status` prints the backend, ABI, mode, roots and reason on all three OSes.
- [ ] Denial and escalation events show up in the logs page (TUI and WebUI) and JSONL.
- [ ] No raw secrets appear in events (redaction test).

## Notes

Depends on: PANDO-US-0040, PANDO-US-0045.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
