package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEngineTemplate_Basic(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: my-agent
description: "Test agent"
command: echo
prompt_mode: arg
prompt_arg: "-p"
model_arg: "--model"
default_model: "v1"
models:
  - id: "v1"
    description: "Version 1"
output_format: text
args:
  - "--no-color"
env:
  NO_COLOR: "1"
`
	path := filepath.Join(dir, "my-agent.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	tpl, err := LoadEngineTemplate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tpl.Name != "my-agent" {
		t.Errorf("Name = %q, want %q", tpl.Name, "my-agent")
	}
	if tpl.Command != "echo" {
		t.Errorf("Command = %q, want %q", tpl.Command, "echo")
	}
	if tpl.effectivePromptMode() != PromptModeArg {
		t.Errorf("PromptMode = %q, want %q", tpl.effectivePromptMode(), PromptModeArg)
	}
	if tpl.effectiveOutputFormat() != OutputFormatText {
		t.Errorf("OutputFormat = %q, want %q", tpl.effectiveOutputFormat(), OutputFormatText)
	}
	if len(tpl.Models) != 1 || tpl.Models[0].ID != "v1" {
		t.Errorf("Models = %v, want [{v1 ...}]", tpl.Models)
	}
}

func TestLoadEngineTemplate_NameFromFilename(t *testing.T) {
	dir := t.TempDir()
	yaml := `
command: echo
output_format: text
`
	path := filepath.Join(dir, "my-fancy-agent.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	tpl, err := LoadEngineTemplate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tpl.Name != "my-fancy-agent" {
		t.Errorf("Name = %q, want %q", tpl.Name, "my-fancy-agent")
	}
}

func TestLoadEngineTemplate_InvalidOutputFormat(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: bad
command: echo
output_format: xml
`
	path := filepath.Join(dir, "bad.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEngineTemplate(path)
	if err == nil {
		t.Error("expected error for invalid output_format, got nil")
	}
}

func TestLoadEngineTemplate_JSONLMissingOutputField(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: jsonl-agent
command: echo
output_format: jsonl
`
	path := filepath.Join(dir, "jsonl-agent.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEngineTemplate(path)
	if err == nil {
		t.Error("expected error when jsonl_output_field is missing, got nil")
	}
}

func TestScanEnginesDir_Empty(t *testing.T) {
	dir := t.TempDir()
	templates, errs := ScanEnginesDir(dir)
	if len(templates) != 0 {
		t.Errorf("expected 0 templates, got %d", len(templates))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %v", errs)
	}
}

func TestScanEnginesDir_NonExistent(t *testing.T) {
	templates, errs := ScanEnginesDir("/nonexistent/path/xyz")
	if len(templates) != 0 {
		t.Errorf("expected 0 templates, got %d", len(templates))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for non-existent dir, got %v", errs)
	}
}

func TestScanEnginesDir_MultipleTemplates(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"agent-a", "agent-b"} {
		content := "name: " + name + "\ncommand: echo\noutput_format: text\n"
		path := filepath.Join(dir, name+".template.yaml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Non-matching file should be ignored.
	_ = os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hello"), 0644)

	templates, errs := ScanEnginesDir(dir)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(templates) != 2 {
		t.Errorf("expected 2 templates, got %d", len(templates))
	}
}

func TestDotGet(t *testing.T) {
	obj := map[string]interface{}{
		"type": "delta",
		"delta": map[string]interface{}{
			"text": "hello",
		},
	}

	val, ok := dotGet(obj, "delta.text")
	if !ok {
		t.Fatal("expected dotGet to find delta.text")
	}
	if val != "hello" {
		t.Errorf("val = %v, want 'hello'", val)
	}

	_, ok = dotGet(obj, "missing")
	if ok {
		t.Error("expected dotGet to return false for missing key")
	}
}

func TestExpandTemplateArg_NoTemplate(t *testing.T) {
	result, err := expandTemplateArg("--no-color", templateVars{})
	if err != nil {
		t.Fatal(err)
	}
	if result != "--no-color" {
		t.Errorf("result = %q, want --no-color", result)
	}
}

func TestExpandTemplateArg_WithVars(t *testing.T) {
	result, err := expandTemplateArg("--work-dir={{.WorkDir}}", templateVars{WorkDir: "/tmp/test"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "--work-dir=/tmp/test" {
		t.Errorf("result = %q, want --work-dir=/tmp/test", result)
	}
}

func TestTemplateRegistry_Empty(t *testing.T) {
	r := NewTemplateRegistry("", "", nil)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if names := r.ListEngines(); len(names) != 0 {
		t.Errorf("expected 0 engines, got %v", names)
	}
	if r.Has("anything") {
		t.Error("expected Has to return false for empty registry")
	}
}

func TestLoadEngineTemplate_EnvVars(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: env-agent
command: echo
output_format: text
env:
  STATIC_KEY: "static-value"
  MODEL_KEY: "{{.Model}}"
env_vars:
  - name: LIST_KEY
    value: "list-value"
  - name: WORK_KEY
    value: "{{.WorkDir}}"
`
	path := filepath.Join(dir, "env-agent.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	tpl, err := LoadEngineTemplate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tpl.Env) != 2 {
		t.Errorf("Env len = %d, want 2", len(tpl.Env))
	}
	if tpl.Env["STATIC_KEY"] != "static-value" {
		t.Errorf("Env[STATIC_KEY] = %q, want 'static-value'", tpl.Env["STATIC_KEY"])
	}
	if len(tpl.EnvVars) != 2 {
		t.Errorf("EnvVars len = %d, want 2", len(tpl.EnvVars))
	}
	if tpl.EnvVars[0].Name != "LIST_KEY" || tpl.EnvVars[0].Value != "list-value" {
		t.Errorf("EnvVars[0] = %+v, want {LIST_KEY list-value}", tpl.EnvVars[0])
	}
}

func TestLoadEngineTemplate_EnvVarMissingName(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: bad-env
command: echo
output_format: text
env_vars:
  - value: "no-name-here"
`
	path := filepath.Join(dir, "bad-env.template.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEngineTemplate(path)
	if err == nil {
		t.Error("expected error for env_vars entry without name, got nil")
	}
}

func TestTemplateRegistry_LoadsTemplates(t *testing.T) {
	dir := t.TempDir()
	logDir := t.TempDir()

	content := "name: test-engine\ncommand: echo\noutput_format: text\n"
	if err := os.WriteFile(filepath.Join(dir, "test-engine.template.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewTemplateRegistry(dir, logDir, nil)
	if !r.Has("test-engine") {
		t.Error("expected registry to have test-engine")
	}
	names := r.ListEngines()
	if len(names) != 1 || names[0] != "test-engine" {
		t.Errorf("ListEngines = %v, want [test-engine]", names)
	}
}
