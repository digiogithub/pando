---
created_at: 2026-07-17T07:46:24.388323943Z
updated_at: 2026-07-17T07:46:24.388323943Z
tags:
    - fix
    - tui
    - sidebar
    - usage
---
# Fix: hide Cost row in TUI sidebar Usage section when cost is zero

## What changed
The `Cost` row in the TUI chat sidebar's Usage section was always rendered, even when
`session.Cost` was `0` (no cost data received, or a provider that reports no cost).
A `$0.0000` line adds no information, so the row is now conditional.

## Files / symbols touched
- `internal/tui/components/chat/sidebar.go` — `(*sidebarCmp).usageSection()`:
  wrapped the `row("Cost", ...)` append in `if m.session.Cost > 0 { ... }`.

This matches the pattern already used in the same function for the
`Cache read/write` and `Reasoning` rows, which are likewise only shown when
their values are non-zero.

## Reason
User feedback: the Usage/Cost block shown in the TUI sidebar (added a few commits ago)
displays a cost line even with no data or a zero value; that line carries no information
and should be hidden.

## Verification
- `go build ./internal/tui/...` — passes with no errors.
- Rows above (`Context` / `Total tokens`, `Input / Output`) are unaffected, so the
  Usage section never renders as a bare title.

Related: [[feature_realtime_context_token_counter]], [[project_tui_chat_info_sidebar_plan]]
