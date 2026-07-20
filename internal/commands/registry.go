// Package commands provides a shared registry of slash commands used across
// ACP, TUI, and WebUI interfaces.
package commands

import (
	"os"
	"path/filepath"
	"strings"
)

// SlashCommand represents a slash command available to users.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AcceptsArgs bool   `json:"acceptsArgs"`
}

// BuiltinCommands returns the built-in slash commands shared across all interfaces.
func BuiltinCommands() []SlashCommand {
	return []SlashCommand{
		{Name: "goal", Description: "Start goal mode with a persistent objective", AcceptsArgs: true},
		{Name: "autopilot", Description: "Alias for /goal", AcceptsArgs: true},
		{Name: "goal-status", Description: "Show the status of the current goal", AcceptsArgs: false},
		{Name: "goal-cancel", Description: "Cancel the current goal execution", AcceptsArgs: false},
		{Name: "compact", Description: "Create a manual compact summary for the current session", AcceptsArgs: false},
		{Name: "summarize", Description: "Alias for /compact", AcceptsArgs: false},
		{Name: "db-compact", Description: "Compact the database (VACUUM) and reclaim free space", AcceptsArgs: false},
		{Name: "ponytail", Description: "Toggle lazy-senior-dev (ponytail) mode: lite|full|ultra|off", AcceptsArgs: true},
		{Name: "caveman", Description: "Shorter answers to cut output tokens: lite|full|ultra", AcceptsArgs: true},
		{Name: "caveman-finish", Description: "Disable caveman output brevity and return to normal output", AcceptsArgs: false},
		{Name: "superpowers", Description: "Enable the opt-in disciplined development workflow (plan-first, verify-always)", AcceptsArgs: true},
		{Name: "superpowers-finish", Description: "Verify and close the active Superpowers workflow, then return to normal mode", AcceptsArgs: false},
		{Name: "learning", Description: "Enable learner mode: read the KB more, document discoveries, ask questions, keep docs current", AcceptsArgs: true},
		{Name: "learning-finish", Description: "Consolidate what was learned into KB/memory and return to normal mode", AcceptsArgs: false},
		{Name: "improve-agents-md", Description: "Create or reinforce AGENTS.md with the mandatory AI-agent operating rules", AcceptsArgs: true},
		{Name: "vulnhunt", Description: "Adversarial security audit: trace attacker input to sinks and report exploitable vulnerabilities", AcceptsArgs: true},
		{Name: "vulnhunter-fix", Description: "Test-driven remediation of confirmed vulnerabilities (exploit -> failing test -> fix -> verify)", AcceptsArgs: true},
		{Name: "vulnhunt-fix-verify", Description: "Read-only independent verification of claimed security fixes, per-finding verdict", AcceptsArgs: true},
	}
}

// AllCommands returns built-in commands plus custom commands discovered from
// the filesystem (user and project command directories).
func AllCommands(dataDir string) []SlashCommand {
	cmds := BuiltinCommands()

	// Load project commands from <dataDir>/commands/
	if dataDir != "" {
		projectDir := filepath.Join(dataDir, "commands")
		custom := loadCustomFromDir(projectDir, "project:")
		cmds = append(cmds, custom...)
	}

	// Load user commands from XDG_CONFIG_HOME/pando/commands or ~/.pando/commands
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			xdgConfigHome = filepath.Join(home, ".config")
		}
	}
	if xdgConfigHome != "" {
		userDir := filepath.Join(xdgConfigHome, "pando", "commands")
		custom := loadCustomFromDir(userDir, "user:")
		cmds = append(cmds, custom...)
	}

	// Also check ~/.pando/commands
	if home, err := os.UserHomeDir(); err == nil {
		homeDir := filepath.Join(home, ".pando", "commands")
		custom := loadCustomFromDir(homeDir, "user:")
		cmds = append(cmds, custom...)
	}

	return cmds
}

// Match filters commands by prefix match on name (case-insensitive).
func Match(query string, cmds []SlashCommand) []SlashCommand {
	if query == "" {
		return cmds
	}
	query = strings.ToLower(query)
	var matched []SlashCommand
	for _, cmd := range cmds {
		if strings.HasPrefix(strings.ToLower(cmd.Name), query) {
			matched = append(matched, cmd)
		}
	}
	return matched
}

// Parse checks if input is a slash command. Returns the command name, arguments,
// and whether parsing succeeded.
func Parse(input string) (name string, args string, ok bool) {
	line := strings.TrimSpace(input)
	if !strings.HasPrefix(line, "/") {
		return "", "", false
	}
	line = line[1:] // strip leading "/"
	if line == "" {
		return "", "", false
	}
	parts := strings.SplitN(line, " ", 2)
	name = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	// Validate against known commands
	for _, cmd := range BuiltinCommands() {
		if cmd.Name == name {
			return name, args, true
		}
	}
	// Also accept custom command names (prefixed with user: or project:)
	if strings.Contains(name, ":") {
		return name, args, true
	}
	return "", "", false
}

// loadCustomFromDir scans a directory for .md files and returns them as slash commands.
func loadCustomFromDir(dir string, prefix string) []SlashCommand {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var cmds []SlashCommand
	_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(fi.Name()), ".md") {
			return nil
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		id := strings.TrimSuffix(relPath, filepath.Ext(relPath))
		id = strings.ReplaceAll(id, string(filepath.Separator), ":")
		cmds = append(cmds, SlashCommand{
			Name:        prefix + id,
			Description: "Custom command: " + prefix + id,
			AcceptsArgs: false,
		})
		return nil
	})
	return cmds
}
