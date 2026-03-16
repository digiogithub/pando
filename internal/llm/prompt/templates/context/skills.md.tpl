{{- if .SkillsMetadata }}
# Available Skills
{{.SkillsMetadata}}

When the user invokes a skill by name, read the skill's full instructions and follow them precisely.
{{- end }}
{{- range .ActiveSkills }}

## Active Skill Instructions
{{.}}
{{- end }}
