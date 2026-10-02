package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

var publicBasePathPattern = regexp.MustCompile(`^/api/v1/projects/[A-Za-z0-9-]+/web$`)

// startupModeProjectChild is the startup mode of a Pando spawned by a parent
// instance to serve one project (PANDO_PARENT_INSTANCE is set).
const startupModeProjectChild = "project-child"

type startupContext struct {
	Mode             string
	ParentInstanceID string
	ProjectID        string
	ProjectName      string
	PublicBasePath   string

	// APIToken is the API token the parent minted for this child. It is read
	// from PANDO_CHILD_API_TOKEN exactly once and removed from the process
	// environment so tools and subprocesses never inherit it.
	APIToken string
	// ParentPID is the pid of the parent process, watched so the child never
	// outlives it. Zero when the parent did not report one.
	ParentPID int
}

// minChildAPITokenLen is the shortest parent-minted token a project child
// accepts; the parent generates 32 random bytes hex encoded.
const minChildAPITokenLen = 32

const (
	childAPITokenEnv = "PANDO_CHILD_API_TOKEN"
	parentPIDEnv     = "PANDO_PARENT_PID"
)

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

	ctx.Mode = startupModeProjectChild
	ctx.APIToken = strings.TrimSpace(os.Getenv(childAPITokenEnv))
	_ = os.Unsetenv(childAPITokenEnv)
	if pid, err := strconv.Atoi(strings.TrimSpace(os.Getenv(parentPIDEnv))); err == nil && pid > 0 {
		ctx.ParentPID = pid
	}
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
