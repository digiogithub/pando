package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/skills/catalog"
)

// handleSnapshotsCount handles GET /api/v1/snapshots/count.
// Returns the number of snapshots currently stored.
func (s *Server) handleSnapshotsCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.AgentVCS == nil {
		writeJSON(w, http.StatusOK, map[string]int{"count": 0})
		return
	}

	sessions, err := s.app.AgentVCS.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions: "+err.Error())
		return
	}

	total := 0
	for _, sid := range sessions {
		n, err := s.app.AgentVCS.SessionCommitCount(r.Context(), sid)
		if err == nil {
			total += n
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{"count": total})
}

// handleRegenerateAPIToken handles POST /api/v1/config/api-server/regenerate-token.
// Generates a new server auth token and returns it.
func (s *Server) handleRegenerateAPIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	newToken := hex.EncodeToString(b)
	s.token = newToken

	writeJSON(w, http.StatusOK, map[string]string{"token": newToken})
}

// InstalledSkillResponse represents a skill installed on disk.
type InstalledSkillResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Source      string `json:"source"`  // "owner/repo" from lock, or "(local)"
	Scope       string `json:"scope"`   // "global", "project", or "(local)"
	Active      bool   `json:"active"`  // loaded in SkillManager
	SkillID     string `json:"skillId"` // from lock, may be empty
}

// handleListInstalledSkills handles GET /api/v1/skills/installed.
// Discovers SKILL.md files across every configured search path (the built-in
// .pando/.claude directories plus any extra paths from [Skills] config),
// enriches with catalog-lock.json data, and reports whether each is active.
func (s *Server) handleListInstalledSkills(w http.ResponseWriter, r *http.Request) {
	workDir := config.WorkingDirectory()

	var extraPaths []string
	if cfg := config.Get(); cfg != nil {
		extraPaths = cfg.Skills.Paths
	}

	// Use the same discovery logic as the agent's skill loader so the UI lists
	// exactly the skills that will actually be loaded, including custom paths.
	paths := skills.ConfiguredDiscoveryPaths(workDir, extraPaths)
	discovered, err := skills.DiscoverSkills(paths)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to discover skills: "+err.Error())
		return
	}

	// Catalog lock files only live in the .pando skills dirs (global + project).
	locks := []*catalog.CatalogLock{}
	for _, dir := range []string{catalog.ResolveSkillsDir(false), catalog.ResolveSkillsDir(true)} {
		if lock, err := catalog.ReadLock(dir); err == nil {
			locks = append(locks, lock)
		}
	}

	home, _ := os.UserHomeDir()

	result := make([]InstalledSkillResponse, 0, len(discovered))
	for _, sk := range discovered {
		name := sk.Metadata.Name

		item := InstalledSkillResponse{
			Name:        name,
			Description: sk.Metadata.Description,
			Version:     sk.Metadata.Version,
			Source:      "(local)",
			Scope:       skillScope(sk.SourcePath, home, workDir),
		}

		// Enrich with catalog lock data if the skill was installed from a catalog.
		for _, lock := range locks {
			if entry, ok := lock.Skills[name]; ok {
				item.Source = entry.Source
				item.Scope = entry.Scope
				item.SkillID = entry.SkillID
				break
			}
		}

		// Check if active in the running SkillManager.
		if s.app.SkillManager != nil {
			item.Active = s.app.SkillManager.IsLoaded(name)
		}

		result = append(result, item)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": result})
}

// skillScope classifies a discovered skill by its source path: skills under the
// user's home directory are "global", those under the working directory are
// "project", and anything else (custom configured paths) is "(local)".
func skillScope(sourcePath, home, workDir string) string {
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, sourcePath); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return "project"
		}
	}
	if home != "" {
		if rel, err := filepath.Rel(home, sourcePath); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return "global"
		}
	}
	return "(local)"
}

// handleSkillsCatalog handles GET /api/v1/skills/catalog?q=<query>.
// Proxies the search to the configured skills.sh catalog. Returns an empty
// list when no query is provided so the web-UI renders gracefully.
func (s *Server) handleSkillsCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"skills": []interface{}{}, "count": 0})
		return
	}

	baseURL := "https://skills.sh"
	if cfg := config.Get(); cfg != nil && cfg.SkillsCatalog.BaseURL != "" {
		baseURL = cfg.SkillsCatalog.BaseURL
	}

	client := catalog.NewClient(baseURL)
	result, err := client.Search(r.Context(), q, 20)
	if err != nil {
		writeError(w, http.StatusBadGateway, "catalog search failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": result.Skills, "count": result.Count})
}

// handleInstallSkill handles POST /api/v1/skills/install.
// Expects JSON body: {"name":"...", "source":"owner/repo", "skillId":"...", "scope":"global"|"project"}.
func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Source  string `json:"source"`
		SkillID string `json:"skillId"`
		Scope   string `json:"scope"` // "global" or "project"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Name == "" || req.Source == "" {
		writeError(w, http.StatusBadRequest, "name and source are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	content, err := catalog.FetchSkillContent(ctx, req.Source, req.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch skill content: "+err.Error())
		return
	}

	global := req.Scope != "project"
	targetDir := catalog.ResolveSkillsDir(!global)

	if err := catalog.InstallSkill(content, req.Name, targetDir); err != nil {
		if errors.Is(err, catalog.ErrSkillAlreadyInstalled) {
			writeError(w, http.StatusConflict, "skill already installed")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to install skill: "+err.Error())
		return
	}

	scope := "global"
	if !global {
		scope = "project"
	}
	entry := catalog.LockEntry{
		Name:        req.Name,
		Source:      req.Source,
		SkillID:     req.SkillID,
		Scope:       scope,
		InstalledAt: time.Now(),
		Checksum:    catalog.ChecksumContent(content),
	}
	_ = catalog.AddLockEntry(targetDir, entry) // best-effort

	writeJSON(w, http.StatusOK, map[string]string{"status": "installed", "name": req.Name, "dir": targetDir})
}

// handleUninstallSkill handles DELETE /api/v1/skills/{name}.
func (s *Server) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "skill name is required")
		return
	}

	// Check global scope first, then project-local.
	for _, global := range []bool{true, false} {
		targetDir := catalog.ResolveSkillsDir(!global)
		if catalog.IsSkillInstalled(name, targetDir) {
			if err := catalog.UninstallSkill(name, targetDir); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to uninstall skill: "+err.Error())
				return
			}
			_ = catalog.RemoveLockEntry(targetDir, name) // best-effort
			writeJSON(w, http.StatusOK, map[string]string{"status": "uninstalled", "name": name})
			return
		}
	}

	writeError(w, http.StatusNotFound, "skill not installed")
}
