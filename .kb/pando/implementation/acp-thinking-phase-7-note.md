# ACP Thinking Phase 7 Note

- Tuned grouped-thinking streaming to flush at `300ms` or `360` buffered characters, which keeps ACP reasoning updates responsive while still avoiding token-level noise.
- Clarified the ACP `thinking_stream_mode` copy so `off`, `grouped`, and `full` communicate final-summary, periodic-block, and noisy-per-chunk behavior directly in the selector UI.
- Added ACP debug logs at the three decision points that matter during development:
  - when the effective thinking stream mode is applied for a prompt stream;
  - when grouped thinking flushes, including the flush reason (`forced`, `time`, `size`) and emitted character count;
  - when ACP session thinking settings are applied to runtime inference overrides before `Run`.
- Phase 6 persistence remains the final source of restored session thinking state; Phase 7 only tunes presentation/logging on top of that normalized persisted state.
