package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

var publicBasePathPattern = regexp.MustCompile(`^/api/v1/projects/[^/]+/web$`)

type startupContext struct {
	Mode             string
	ParentInstanceID string
	ProjectID        string
	ProjectName      string
	PublicBasePath   string
}

func resolveStartupContext(cwd, defaultMode string) startupContext {
	ctx := startupContext{
		Mode:             defaultMode,
		ParentInstanceID: strings.TrimSpace(os.Getenv("PANDO_PARENT_INSTANCE")),
		ProjectID:        strings.TrimSpace(os.Getenv("PANDO_PROJECT_ID")),
		PublicBasePath:   resolvePublicBasePath(),
	}
	if ctx.ParentInstanceID == "" {
		ctx.PublicBasePath = ""
		return ctx
	}

	ctx.Mode = "project-child"
	ctx.ProjectName = lookupProjectName(cwd)
	if !publicBasePathPattern.MatchString(ctx.PublicBasePath) {
		ctx.PublicBasePath = ""
	}
	return ctx
}

func resolvePublicBasePath() string {
	return strings.TrimSpace(os.Getenv("PANDO_PUBLIC_BASE"))
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
