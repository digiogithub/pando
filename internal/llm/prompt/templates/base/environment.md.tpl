{{/* Environment information */}}
# Environment
<env>
Working directory: {{ .WorkingDir }}
Platform: {{ .Platform }}
Date: {{ .Date }}
{{- if .IsGitRepo }}
Git repository: yes
{{- end }}
</env>
