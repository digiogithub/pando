{{/* Environment information */}}
# Environment
<env>
Working directory: {{.WorkingDir}}
Is directory a git repo: {{if .IsGitRepo}}Yes{{else}}No{{end}}
Platform: {{.Platform}}
Today's date: {{.Date}}
</env>
{{- if .ProjectListing }}
<project>
{{.ProjectListing}}
</project>
{{- end }}