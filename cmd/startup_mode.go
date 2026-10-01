package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

type startupContext struct {
	Mode             string
	ParentInstanceID string
	ProjectID        string
	ProjectName      string
}

func resolveStartupContext(cwd, defaultMode string) startupContext {
	ctx := startupContext{
		Mode:             defaultMode,
		ParentInstanceID: strings.TrimSpace(os.Getenv("PANDO_PARENT_INSTANCE")),
		ProjectID:        strings.TrimSpace(os.Getenv("PANDO_PROJECT_ID")),
	}
	if ctx.ParentInstanceID == "" {
		return ctx
	}

	ctx.Mode = "project-child"
	ctx.ProjectName = lookupProjectName(cwd)
	return ctx
}

func lookupProjectName(cwd string) string {
	canonical := config.CanonicalProjectPath(cwd)
	if canonical != "" {
		if entries, err := config.LoadGlobalProjects(); err == nil {
			for _, entry := range entries {
				if config.CanonicalProjectPath(entry.Path) == canonical && strings.TrimSpace(entry.Name) != "" {
					return strings.TrimSpace(entry.Name)
				}
			}
		}
	}

	base := filepath.Base(canonical)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}
