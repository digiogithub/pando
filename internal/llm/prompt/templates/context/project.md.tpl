{{- if .ContextFiles }}
# Project-Specific Context
Follow the instructions in the project context below.
{{- range .ContextFiles }}

## From: {{.Path}}
{{.Content}}
{{- end }}
{{- end }}
