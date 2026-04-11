# Web UI terminal parity plan

Goal: make web-ui terminal behave more like TUI.

Phases:
1. Inspect TUI terminal and web-ui terminal/API implementations.
2. Backend: improve shell selection and command execution context for interactive shells and colored output; add terminal session concept if needed for tab persistence.
3. Frontend: add terminal tabs and xterm-based rendering so ANSI colors are displayed.
4. Verify with typecheck/build/tests.

Acceptance criteria:
- Web UI can create multiple terminal tabs.
- Web UI uses bash when available (or zsh on macOS, otherwise shell fallback), not hardcoded sh.
- Colored terminal output is preserved/rendered.
- Existing security boundaries for dangerous commands remain in place.