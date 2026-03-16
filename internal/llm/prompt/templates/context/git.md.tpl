{{- if .IsGitRepo }}
# Git Information
{{- if .GitBranch }}
Current branch: {{.GitBranch}}
{{- end }}
{{- if .GitStatus }}

Status:
{{.GitStatus}}
{{- end }}
{{- if .GitRecentCommits }}

Recent commits:
{{.GitRecentCommits}}
{{- end }}
{{- end }}
