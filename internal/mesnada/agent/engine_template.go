// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PromptMode controls how the prompt is passed to the CLI process.
type PromptMode string

const (
	// PromptModeArg passes the prompt as a CLI argument (default).
	PromptModeArg PromptMode = "arg"
	// PromptModeStdin pipes the prompt to the process stdin.
	PromptModeStdin PromptMode = "stdin"
)

// OutputFormat describes the expected output format from the CLI process.
type OutputFormat string

const (
	// OutputFormatText treats output as plain text (default).
	OutputFormatText OutputFormat = "text"
	// OutputFormatJSONL parses output as newline-delimited JSON.
	OutputFormatJSONL OutputFormat = "jsonl"
)

// EngineTemplateModel defines a model available for a custom engine.
type EngineTemplateModel struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

// EngineTemplate defines a user-supplied custom agent engine loaded from a
// <name>.template.yaml file in the engines directory.
type EngineTemplate struct {
	// Name is the engine identifier used in spawn calls (no spaces).
	// Defaults to the filename stem when omitted.
	Name string `yaml:"name"`

	// Description is a human-readable summary shown in tool descriptions.
	Description string `yaml:"description"`

	// Command is the binary to execute (resolved via PATH or absolute path).
	Command string `yaml:"command"`

	// Args are fixed CLI arguments always prepended before any dynamic args.
	// Values may include Go template expressions: {{.Model}}, {{.WorkDir}},
	// {{.TaskID}}, {{.LogFile}}.
	Args []string `yaml:"args"`

	// PromptMode controls how the prompt is delivered: "arg" (default) or "stdin".
	PromptMode PromptMode `yaml:"prompt_mode"`

	// PromptArg is the CLI flag used to pass the prompt when PromptMode is "arg".
	// Defaults to "-p".
	PromptArg string `yaml:"prompt_arg"`

	// ModelArg is the CLI flag used to pass the model ID (e.g. "--model").
	// When empty the model is not passed as an argument.
	ModelArg string `yaml:"model_arg"`

	// DefaultModel is the model used when none is specified in the spawn request.
	DefaultModel string `yaml:"default_model"`

	// Models lists the models available for this engine.
	Models []EngineTemplateModel `yaml:"models"`

	// OutputFormat is "text" (default) or "jsonl".
	OutputFormat OutputFormat `yaml:"output_format"`

	// JSONLOutputField is a dot-delimited path to the string field that contains
	// the human-readable output within each JSONL line (e.g. "delta.text").
	// Only used when OutputFormat is "jsonl".
	JSONLOutputField string `yaml:"jsonl_output_field"`

	// JSONLFilterField is an optional field name used to filter JSONL lines.
	// Only lines where JSONLFilterField == JSONLFilterValue are processed.
	JSONLFilterField string `yaml:"jsonl_filter_field"`

	// JSONLFilterValue is the required value for JSONLFilterField.
	JSONLFilterValue string `yaml:"jsonl_filter_value"`

	// Env holds additional environment variables set for the spawned process.
	// Values support Go template expressions: {{.Model}}, {{.WorkDir}}, {{.TaskID}}, {{.LogFile}}.
	//
	//   env:
	//     MY_KEY: "static-value"
	//     MODEL_ID: "{{.Model}}"
	Env map[string]string `yaml:"env"`

	// EnvVars is an alternative list-style notation for environment variables.
	// Useful when the explicit name/value structure is preferred over the map form.
	// Both Env and EnvVars may be used simultaneously; EnvVars is applied last.
	// Values support the same Go template expressions as Env.
	//
	//   env_vars:
	//     - name: MY_KEY
	//       value: "static-value"
	//     - name: MODEL_ID
	//       value: "{{.Model}}"
	EnvVars []EnvVar `yaml:"env_vars"`
}

// EnvVar defines a single environment variable with an explicit name and value.
type EnvVar struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// Validate checks that all required fields are present and values are valid.
func (t *EngineTemplate) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("engine template: name is required")
	}
	if strings.ContainsAny(t.Name, " \t\n") {
		return fmt.Errorf("engine template %q: name must not contain whitespace", t.Name)
	}
	if t.Command == "" {
		return fmt.Errorf("engine template %q: command is required", t.Name)
	}
	switch t.PromptMode {
	case "", PromptModeArg, PromptModeStdin:
		// valid
	default:
		return fmt.Errorf("engine template %q: invalid prompt_mode %q (must be 'arg' or 'stdin')", t.Name, t.PromptMode)
	}
	switch t.OutputFormat {
	case "", OutputFormatText, OutputFormatJSONL:
		// valid
	default:
		return fmt.Errorf("engine template %q: invalid output_format %q (must be 'text' or 'jsonl')", t.Name, t.OutputFormat)
	}
	if t.OutputFormat == OutputFormatJSONL && t.JSONLOutputField == "" {
		return fmt.Errorf("engine template %q: jsonl_output_field is required when output_format is 'jsonl'", t.Name)
	}
	for i, ev := range t.EnvVars {
		if ev.Name == "" {
			return fmt.Errorf("engine template %q: env_vars[%d] is missing 'name'", t.Name, i)
		}
	}
	return nil
}

// effectivePromptMode returns PromptModeArg when PromptMode is unset.
func (t *EngineTemplate) effectivePromptMode() PromptMode {
	if t.PromptMode == "" {
		return PromptModeArg
	}
	return t.PromptMode
}

// effectivePromptArg returns "-p" when PromptArg is unset.
func (t *EngineTemplate) effectivePromptArg() string {
	if t.PromptArg == "" {
		return "-p"
	}
	return t.PromptArg
}

// effectiveOutputFormat returns OutputFormatText when OutputFormat is unset.
func (t *EngineTemplate) effectiveOutputFormat() OutputFormat {
	if t.OutputFormat == "" {
		return OutputFormatText
	}
	return t.OutputFormat
}

// LoadEngineTemplate reads a single template YAML file.
// If the Name field is empty the filename stem is used as the engine name.
func LoadEngineTemplate(path string) (*EngineTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load engine template %q: %w", path, err)
	}

	var tpl EngineTemplate
	if err := yaml.Unmarshal(data, &tpl); err != nil {
		return nil, fmt.Errorf("parse engine template %q: %w", path, err)
	}

	// Derive name from filename stem when omitted.
	if tpl.Name == "" {
		base := filepath.Base(path)
		// Strip all extensions: "my-agent.template.yaml" → "my-agent"
		for ext := filepath.Ext(base); ext != ""; ext = filepath.Ext(base) {
			base = strings.TrimSuffix(base, ext)
		}
		tpl.Name = base
	}

	if err := tpl.Validate(); err != nil {
		return nil, err
	}

	return &tpl, nil
}

// ScanEnginesDir scans dir for files matching the pattern *.template.yaml and
// returns one EngineTemplate per valid file. Errors from individual files are
// logged and skipped; the caller receives only successfully loaded templates.
func ScanEnginesDir(dir string) ([]*EngineTemplate, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("scan engines dir %q: %w", dir, err)}
	}

	var templates []*EngineTemplate
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".template.yaml") && !strings.HasSuffix(name, ".template.yml") {
			continue
		}

		tpl, err := LoadEngineTemplate(filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		templates = append(templates, tpl)
	}

	return templates, errs
}
