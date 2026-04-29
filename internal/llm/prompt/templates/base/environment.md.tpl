{{/* Environment information */}}
# Environment
<env>
Working directory: {{.WorkingDir}}
Is directory a git repo: {{if .IsGitRepo}}Yes{{else}}No{{end}}
{{- if and .IsGitRepo .GitBranch}}
Current git branch: {{.GitBranch}}
{{- end}}
Platform: {{.Platform}}
Current date and time: {{.Date}}
</env>
{{- if .ProjectListing }}
<project>
{{.ProjectListing}}
</project>
{{- end }}