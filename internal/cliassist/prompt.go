package cliassist

import (
	"fmt"
	"strings"
)

// BuildSystemPrompt creates the system message instructing the LLM to return only a shell command.
func BuildSystemPrompt(info SysInfo) string {
	return fmt.Sprintf(`You are a shell command expert for %s using %s.
Your task is to generate a single shell command or short script that accomplishes what the user asks.

Rules:
- Output ONLY the command or script, nothing else. No explanations, no markdown code fences.
- If the task requires multiple commands, chain them with && or write a brief shell script.
- Use idiomatic %s syntax.
- Prefer portable, safe commands. Do not use destructive operations unless explicitly asked.
- If a one-liner is not possible, output a multi-line script that can be piped to %s.`,
		info.OS, info.ShellName, info.ShellName, info.ShellName)
}

// BuildUserPrompt joins the user's CLI args into a single request string.
func BuildUserPrompt(args []string) string {
	return strings.Join(args, " ")
}

// CleanCommand strips markdown code fences from a model response, in case the model
// disobeys the "no fences" instruction.
func CleanCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 2 {
			lines = lines[1:] // remove opening fence line (e.g. "```bash")
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1] // remove closing fence
			}
			raw = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return raw
}
