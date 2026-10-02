package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/llm/models"
)

// Setup scopes accepted by PrepareSetupScope.
const (
	SetupScopeGlobal  = "global"
	SetupScopeProject = "project"
)

// DefaultSetupCodeEmbeddingModel is the code embedding model the first-run
// assistant suggests pulling into Ollama. It matches the value written by the
// annotated config templates.
const DefaultSetupCodeEmbeddingModel = "hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M"

// DefaultSetupDocumentEmbeddingModel is the document (RAG) embedding model the
// first-run assistant suggests pulling into Ollama.
const DefaultSetupDocumentEmbeddingModel = "nomic-embed-text"

// globalSetupConfigHeader is written to a brand-new global config file created
// by the first-run assistant. Every value is then persisted by the regular
// UpdateXxx funnels, so the file only needs to exist and parse.
const globalSetupConfigHeader = `# Pando global profile configuration.
# Created by the first-run setup assistant. These settings apply to every
# directory that has no project-local .pando.toml of its own.
`

// ErrConfigNotLoaded is returned while no configuration is in effect, which
// happens transiently during Reload. Callers may retry.
var ErrConfigNotLoaded = errors.New("config not loaded")

// SetupStatus describes what the first-run assistant needs to know about the
// current working directory and the configuration that applies to it.
type SetupStatus struct {
	WorkingDir       string `json:"workingDir"`
	IsHomeDir        bool   `json:"isHomeDir"`
	HasLocalConfig   bool   `json:"hasLocalConfig"`
	LocalConfigPath  string `json:"localConfigPath,omitempty"`
	HasGlobalConfig  bool   `json:"hasGlobalConfig"`
	GlobalConfigPath string `json:"globalConfigPath,omitempty"`
	// DefaultGlobalConfigPath is where a new global file would be created.
	DefaultGlobalConfigPath string `json:"defaultGlobalConfigPath,omitempty"`
	// ProviderAccounts counts the accounts the user configured (auto-detected
	// local runtimes excluded).
	ProviderAccounts    int    `json:"providerAccounts"`
	HasUsableProvider   bool   `json:"hasUsableProvider"`
	CoderModel          string `json:"coderModel,omitempty"`
	CoderModelValid     bool   `json:"coderModelValid"`
	RemembrancesEnabled bool   `json:"remembrancesEnabled"`
	// Completed is true once the assistant has been finished on this machine.
	Completed bool `json:"completed"`
	// Needed is true when the assistant should open by itself: no project
	// config applies to this directory, the assistant was never finished, and
	// the configuration in effect has no explicitly configured provider
	// account or no valid main model. Providers Pando only auto-detected (a
	// local Ollama, an API key in the environment) do not count: the user
	// never chose them.
	Needed bool `json:"needed"`
}

// GetSetupStatus computes the first-run assistant status for the current
// working directory.
func GetSetupStatus() (SetupStatus, error) {
	if cfg == nil {
		return SetupStatus{}, ErrConfigNotLoaded
	}

	status := SetupStatus{
		WorkingDir:          cfg.WorkingDir,
		IsHomeDir:           IsHomeDirectory(cfg.WorkingDir),
		ProviderAccounts:    countExplicitProviderAccounts(cfg.ProviderAccounts),
		RemembrancesEnabled: cfg.Remembrances.Enabled,
	}

	if local := FindLocalConfigFile(cfg.WorkingDir); local != "" {
		status.HasLocalConfig = true
		status.LocalConfigPath = local
	}
	if global, err := resolveGlobalConfigFilePath(); err == nil && global != "" {
		status.HasGlobalConfig = true
		status.GlobalConfigPath = global
	}
	if def, err := defaultGlobalSetupConfigPath(); err == nil {
		status.DefaultGlobalConfigPath = def
	}

	for _, provider := range defaultProviderPreference() {
		if providerUsableForDefaults(provider) {
			status.HasUsableProvider = true
			break
		}
	}

	if coder, ok := cfg.Agents[AgentCoder]; ok && coder.Model != "" {
		status.CoderModel = string(coder.Model)
		if _, supported := models.SupportedModels()[coder.Model]; supported {
			status.CoderModelValid = validateAgent(cfg, AgentCoder, coder) == nil
		}
	}

	status.Completed = SetupCompleted()
	status.Needed = !status.HasLocalConfig && !status.Completed &&
		(status.ProviderAccounts == 0 || !status.CoderModelValid)
	return status, nil
}

// setupCompletedMarkerPath is the per-user file recording that the first-run
// assistant was finished. It lives next to the stored OAuth sessions, outside
// any config file, so it is never copied into a project.
func setupCompletedMarkerPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "setup-completed"), nil
}

// SetupCompleted reports whether the first-run assistant was finished.
func SetupCompleted() bool {
	path, err := setupCompletedMarkerPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// MarkSetupCompleted records that the first-run assistant was finished, so it
// no longer opens by itself in directories without a project config.
func MarkSetupCompleted() error {
	path, err := setupCompletedMarkerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.WriteFile(path, []byte("1\n"), 0o644)
}

// countExplicitProviderAccounts counts the provider accounts the user
// configured. A local runtime Load auto-detected (Ollama or llama.cpp
// answering on its default port) is registered as a bare provider with no
// base URL or key and surfaces as an account too; it is not a choice the user
// made, so it must not keep the assistant from opening on a fresh machine.
func countExplicitProviderAccounts(accounts []ProviderAccount) int {
	n := 0
	for _, account := range accounts {
		switch account.Type {
		case models.ProviderOllama, models.ProviderLlamaCpp:
			if strings.TrimSpace(account.BaseURL) == "" && strings.TrimSpace(account.APIKey) == "" {
				continue
			}
		}
		n++
	}
	return n
}

// defaultGlobalSetupConfigPath is the global file the assistant creates when
// none exists yet: ~/.config/pando/.pando.toml (one of the locations Load
// searches, and the one `pando init --target profile` writes).
func defaultGlobalSetupConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", appName, localConfigFilename), nil
}

// PrepareSetupScope makes sure the configuration file for the chosen scope
// exists and is the one every later write lands in, then reloads the
// configuration so the running process sees it.
//
// Writes go through updateCfgFile, which targets the project-local file when
// one applies to the working directory and the global file otherwise. So:
//   - "project" creates the project layout (.pando.toml, .pando/…) in the
//     working directory; from then on writes go to that file.
//   - "global" leaves the working directory untouched and creates the global
//     file when none exists, so writes go to it instead of a fresh
//     ~/.pando.json. It is refused when a project file already applies,
//     because writes would silently go to that file instead.
//
// It returns the path of the file that will receive the settings.
func PrepareSetupScope(scope string) (string, error) {
	if cfg == nil {
		return "", ErrConfigNotLoaded
	}

	// createdFiles are the config files this call brought into existence, so a
	// failed reload can take them back out (never a pre-existing file).
	var createdFiles []string

	switch scope {
	case SetupScopeProject:
		if IsHomeDirectory(cfg.WorkingDir) {
			return "", fmt.Errorf("project settings cannot be created in the home directory; use global settings instead")
		}
		projectFile := filepath.Join(cfg.WorkingDir, localConfigFilename)
		_, statErr := os.Stat(projectFile)
		existed := statErr == nil
		if err := InitializeProjectAt(cfg.WorkingDir); err != nil {
			if !existed {
				rollbackSetupScope([]string{projectFile})
			}
			return "", err
		}
		if !existed {
			createdFiles = append(createdFiles, projectFile)
		}
	case SetupScopeGlobal:
		if local := FindLocalConfigFile(cfg.WorkingDir); local != "" {
			return "", fmt.Errorf("a project config already applies to this directory (%s); global settings would be overridden by it", local)
		}
		existing, err := resolveGlobalConfigFilePath()
		if err != nil {
			return "", err
		}
		if existing == "" {
			path, err := defaultGlobalSetupConfigPath()
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "", fmt.Errorf("create global config directory: %w", err)
			}
			if err := os.WriteFile(path, []byte(globalSetupConfigHeader), 0o644); err != nil {
				return "", fmt.Errorf("write global config file: %w", err)
			}
			createdFiles = append(createdFiles, path)
		}
	default:
		return "", fmt.Errorf("unknown setup scope %q (expected %q or %q)", scope, SetupScopeGlobal, SetupScopeProject)
	}

	// Reload so viper registers the (possibly new) global file as the one in
	// use and the project file, if created, is merged on top.
	if err := Reload(); err != nil {
		rollbackSetupScope(createdFiles)
		return "", fmt.Errorf("reload configuration: %w", err)
	}
	return ResolveConfigFilePath()
}

// rollbackSetupScope removes the config files a failed PrepareSetupScope
// created and reloads, so viper and the configuration go back to the file set
// that applied before the call. A reload failure here is ignored: the caller
// already has the original error to report.
func rollbackSetupScope(createdFiles []string) {
	for _, path := range createdFiles {
		_ = os.Remove(path)
	}
	_ = Reload()
}

// SuggestSetupModels proposes a main (coder) model and a fast, cheap
// secondary model for a provider, from the models currently registered for
// it. Either value is empty when the provider has no suitable model yet (for
// example before its model list has been fetched).
func SuggestSetupModels(provider models.ModelProvider) (mainModel, fastModel string) {
	if best, ok := bestModelForProvider(provider); ok {
		mainModel = string(best.ID)
	}

	candidates := make([]models.Model, 0, 16)
	for _, model := range models.SupportedModels() {
		if model.Provider != provider || isNonChatModel(model) {
			continue
		}
		candidates = append(candidates, model)
	}
	if len(candidates) == 0 {
		return mainModel, ""
	}
	// Small variants first, then the cheaper one, then the larger context.
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if aSmall, bSmall := isSmallModel(a), isSmallModel(b); aSmall != bSmall {
			return aSmall
		}
		aCost, bCost := a.CostPer1MIn+a.CostPer1MOut, b.CostPer1MIn+b.CostPer1MOut
		if aCost != bCost {
			// An unknown price (0) must not win over a known cheap one.
			if aCost == 0 {
				return false
			}
			if bCost == 0 {
				return true
			}
			return aCost < bCost
		}
		if a.ContextWindow != b.ContextWindow {
			return a.ContextWindow > b.ContextWindow
		}
		return a.ID < b.ID
	})
	fastModel = string(candidates[0].ID)
	if !isSmallModel(candidates[0]) && mainModel != "" {
		// No cheap variant exists: reuse the main model rather than pick an
		// arbitrary large one.
		fastModel = mainModel
	}
	return mainModel, fastModel
}

// SecondaryAgentNames are the agents that run the fast/cheap model chosen in
// the first-run assistant: every built-in agent except the coder.
func SecondaryAgentNames() []AgentName {
	names := make([]AgentName, 0, len(KnownAgentNames))
	for _, name := range KnownAgentNames {
		if name != AgentCoder {
			names = append(names, name)
		}
	}
	return names
}

// ApplySetupRemembrances enables Remembrances with local Ollama embedding
// models, keeping every other Remembrances setting as it is.
func ApplySetupRemembrances(documentModel, codeModel string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	documentModel = strings.TrimSpace(documentModel)
	codeModel = strings.TrimSpace(codeModel)
	if documentModel == "" {
		documentModel = DefaultSetupDocumentEmbeddingModel
	}
	if codeModel == "" {
		codeModel = documentModel
	}

	next := cfg.Remembrances
	next.Enabled = true
	next.DocumentEmbeddingProvider = string(models.ProviderOllama)
	next.DocumentEmbeddingModel = documentModel
	next.CodeEmbeddingProvider = string(models.ProviderOllama)
	next.CodeEmbeddingModel = codeModel
	next.UseSameModel = documentModel == codeModel
	return UpdateRemembrances(next)
}
