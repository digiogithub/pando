package tools

import "strings"

// crossModelAliases maps alternative tool names used by different model families
// to the canonical tool names used in Pando. Models trained on Anthropic, OpenAI,
// or other providers may emit tool names that differ from what Pando registers.
//
// Key: alias name the model may call. Value: canonical tool name in Pando.
var crossModelAliases = map[string]string{
	// File reading: Anthropic-trained and OpenAI-trained models use "read"
	"read": "view",

	// File editing: Copilot / GitHub Copilot variants
	"str_replace_editor":      "edit",
	"str_replace_based_edit":  "edit",
	"insert_edit_into_file":   "edit",
	"apply_edit":              "edit",

	// File writing / creation
	"create_file":   "write",
	"write_file":    "write",
	"overwrite_file": "write",

	// Shell execution
	"run_command":      "bash",
	"execute_command":  "bash",
	"run_terminal_cmd": "bash",
	"terminal":         "bash",
	"shell":            "bash",

	// File search / glob
	"find_files":       "glob",
	"list_files":       "glob",
	"find":             "glob",

	// Content search / grep
	"search_files":     "grep",
	"search_in_files":  "grep",
	"search_code":      "grep",

	// Directory listing
	"list_directory": "ls",
	"list_dir":       "ls",
	"dir":            "ls",
}

// ResolveToolAlias returns the canonical tool name for the given name.
// If name is already canonical or unknown, it is returned unchanged.
func ResolveToolAlias(name string) string {
	if canonical, ok := crossModelAliases[strings.ToLower(name)]; ok {
		return canonical
	}
	return name
}

// CrossModelAliasNames returns the list of all known alias names (non-canonical).
// Used to ensure aliases are treated as non-deferred in the tool selection policy.
func CrossModelAliasNames() []string {
	names := make([]string, 0, len(crossModelAliases))
	for alias := range crossModelAliases {
		names = append(names, alias)
	}
	return names
}
