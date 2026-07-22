package commands

import (
	"os"
	"path/filepath"
	"strings"
)

// commandDir pairs a directory holding custom command markdown files with the
// name prefix its commands are exposed under.
type commandDir struct {
	Dir    string
	Prefix string
}

// customCommandDirs returns every directory scanned for custom commands, in the
// same order AllCommands walks them: project first, then the user directories.
func customCommandDirs(dataDir string) []commandDir {
	var dirs []commandDir

	if dataDir != "" {
		dirs = append(dirs, commandDir{Dir: filepath.Join(dataDir, "commands"), Prefix: "project:"})
	}

	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			xdgConfigHome = filepath.Join(home, ".config")
		}
	}
	if xdgConfigHome != "" {
		dirs = append(dirs, commandDir{Dir: filepath.Join(xdgConfigHome, "pando", "commands"), Prefix: "user:"})
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, commandDir{Dir: filepath.Join(home, ".pando", "commands"), Prefix: "user:"})
	}

	return dirs
}

// Content returns the markdown body of a custom command (a name such as
// "project:review" or "user:tools/lint"). It reports false when the name is not
// a custom command or no matching file exists.
func Content(dataDir string, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !strings.Contains(name, ":") {
		return "", false
	}

	for _, dir := range customCommandDirs(dataDir) {
		if !strings.HasPrefix(name, dir.Prefix) {
			continue
		}
		id := strings.TrimPrefix(name, dir.Prefix)
		if id == "" {
			continue
		}
		// Command ids encode nesting with ":"; map it back to a relative path.
		rel := filepath.FromSlash(strings.ReplaceAll(id, ":", "/"))
		path := filepath.Join(dir.Dir, rel+".md")
		// Reject ids that escape the command directory (e.g. "user:../../secrets").
		if !isWithin(dir.Dir, path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return string(data), true
	}
	return "", false
}

// isWithin reports whether path resolves inside dir.
func isWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
