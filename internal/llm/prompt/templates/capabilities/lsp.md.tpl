{{- if .HasLSP }}
# LSP Diagnostics

Language Server Protocol diagnostics are enabled for this project, providing real-time linting and type checking.

## How Diagnostics Appear
- Diagnostics are automatically included in tool output
- File-level diagnostics appear in `<file_diagnostics>` tags
- Project-level diagnostics appear in `<project_diagnostics>` tags

## How to Use Diagnostics
- Fix all diagnostics caused by YOUR changes
- Ignore pre-existing diagnostics in files you did not modify
- Ignore diagnostics unrelated to your changes unless the user asks you to fix them
- Use diagnostics to catch type errors, unused variables, and style issues early
{{- end }}
