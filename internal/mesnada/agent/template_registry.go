// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// EngineTemplateInfo holds metadata about a loaded custom engine used for
// building tool descriptions.
type EngineTemplateInfo struct {
	Name         string
	Description  string
	DefaultModel string
	Models       []string
}

// TemplateRegistry loads and stores custom engine templates from a directory.
type TemplateRegistry struct {
	enginesDir string
	spawners   map[string]*TemplateSpawner
	infos      []EngineTemplateInfo
}

// NewTemplateRegistry creates a registry and scans enginesDir for *.template.yaml
// files. Errors from individual files are logged and skipped; the registry
// remains usable for all successfully loaded templates.
// Returns a non-nil registry even when enginesDir does not exist.
func NewTemplateRegistry(enginesDir, logDir string, onComplete func(*models.Task)) *TemplateRegistry {
	r := &TemplateRegistry{
		enginesDir: enginesDir,
		spawners:   make(map[string]*TemplateSpawner),
	}

	if enginesDir == "" {
		return r
	}

	templates, errs := ScanEnginesDir(enginesDir)
	for _, err := range errs {
		log.Printf("WARN: custom engine template: %v", err)
	}

	for _, tpl := range templates {
		spawner := NewTemplateSpawner(tpl, logDir, onComplete)
		r.spawners[tpl.Name] = spawner

		info := EngineTemplateInfo{
			Name:         tpl.Name,
			Description:  tpl.Description,
			DefaultModel: tpl.DefaultModel,
		}
		for _, m := range tpl.Models {
			info.Models = append(info.Models, m.ID)
		}
		r.infos = append(r.infos, info)

		log.Printf("INFO: loaded custom engine template %q (command=%q, output=%s, prompt=%s)",
			tpl.Name, tpl.Command, tpl.effectiveOutputFormat(), tpl.effectivePromptMode())
	}

	// Sort for deterministic output in tool descriptions.
	sort.Slice(r.infos, func(i, j int) bool {
		return r.infos[i].Name < r.infos[j].Name
	})

	return r
}

// Get returns the spawner for the named engine, if loaded.
func (r *TemplateRegistry) Get(engineName string) (*TemplateSpawner, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.spawners[engineName]
	return s, ok
}

// Has reports whether the named engine is registered.
func (r *TemplateRegistry) Has(engineName string) bool {
	if r == nil {
		return false
	}
	_, ok := r.spawners[engineName]
	return ok
}

// ListEngines returns the names of all loaded custom engines, sorted.
func (r *TemplateRegistry) ListEngines() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.spawners))
	for name := range r.spawners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Infos returns metadata for all loaded engines, sorted by name.
func (r *TemplateRegistry) Infos() []EngineTemplateInfo {
	if r == nil {
		return nil
	}
	return r.infos
}

// Shutdown cancels all running processes across every template spawner.
func (r *TemplateRegistry) Shutdown() {
	if r == nil {
		return
	}
	for _, s := range r.spawners {
		s.Shutdown()
	}
}

// RunningCount returns the total number of active processes across all template spawners.
func (r *TemplateRegistry) RunningCount() int {
	if r == nil {
		return 0
	}
	total := 0
	for _, s := range r.spawners {
		total += s.RunningCount()
	}
	return total
}

// EnsureEnginesDirExists creates the engines directory and drops a README when
// it does not yet exist.
func EnsureEnginesDirExists(enginesDir string) error {
	if enginesDir == "" {
		return nil
	}
	if _, err := os.Stat(enginesDir); err == nil {
		return nil // already exists
	}
	if err := os.MkdirAll(enginesDir, 0755); err != nil {
		return fmt.Errorf("create engines dir %q: %w", enginesDir, err)
	}

	readmePath := filepath.Join(enginesDir, "README.md")
	readme := `# Mesnada Custom Engine Templates

Place ` + "`*.template.yaml`" + ` files here to define custom agent engines.

Each file registers a new engine available in ` + "`mesnada_spawn_agent`" + `:

` + "```yaml" + `
name: my-agent          # engine ID used in spawn calls
description: "My custom agent"
command: my-agent-cli   # binary (resolved via PATH) or absolute path

prompt_mode: arg        # "arg" (default) or "stdin"
prompt_arg: "-p"        # CLI flag for the prompt (when prompt_mode=arg)
model_arg: "--model"    # CLI flag for the model (omit to skip)

default_model: "my-model-v1"
models:
  - id: "my-model-v1"
    description: "Stable version"

output_format: text     # "text" (default) or "jsonl"

# JSONL settings (only when output_format=jsonl):
jsonl_output_field: "delta.text"
jsonl_filter_field: "type"
jsonl_filter_value: "content_block_delta"

args:                   # fixed CLI args always prepended
  - "--no-color"

# Environment variables — map style (both forms can be used together):
env:
  NO_COLOR: "1"
  MY_API_KEY: "secret"
  MODEL_ID: "{{.Model}}"     # supports template vars

# Environment variables — list style (explicit name/value):
env_vars:
  - name: WORK_DIR
    value: "{{.WorkDir}}"
  - name: TASK_ID
    value: "{{.TaskID}}"
  - name: STATIC_FLAG
    value: "enabled"
` + "```" + `

## Available template variables

Both ` + "`args`" + ` values and environment variable values support these expressions:

| Variable        | Description                        |
|-----------------|------------------------------------|
| ` + "`{{.Model}}`" + `   | Model ID passed to the task        |
| ` + "`{{.WorkDir}}`" + ` | Task working directory             |
| ` + "`{{.TaskID}}`" + `  | Unique task identifier             |
| ` + "`{{.LogFile}}`" + ` | Path to the task log file          |

See the Pando documentation for full reference.
`
	_ = os.WriteFile(readmePath, []byte(readme), 0644)
	return nil
}
