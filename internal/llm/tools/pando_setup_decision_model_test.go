package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

const dmSecret = "sk-decision-secret-9876"

// withDecisionModelConfig loads an isolated config (own HOME and project dir)
// and returns the project and home dirs so a test can inspect what was written to disk.
func withDecisionModelConfig(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return []string{dir, home}
}

func dmRun(t *testing.T, args string) ToolResponse {
	t.Helper()
	return runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("s"), "decision-model", args)
}

func TestPandoSetupDecisionModelListedInHelp(t *testing.T) {
	if !strings.Contains(renderSetupHelp(""), "decision-model") {
		t.Fatal("help does not list decision-model")
	}
	if !strings.Contains(NewPandoSetupTool(nil, nil).Info().Description, "decision-model") {
		t.Fatal("tool description does not mention decision-model")
	}
}

func TestPandoSetupDecisionModelSetShowTest(t *testing.T) {
	dirs := withDecisionModelConfig(t)
	srv := systemonetest.New(t)

	resp := dmRun(t, "set --provider ollama --base-url "+srv.BaseURL()+" --model tev1:0.8b --timeout-ms 2500 --api-key "+dmSecret)
	if resp.IsError {
		t.Fatalf("set failed: %s", resp.Content)
	}
	if strings.Contains(resp.Content, dmSecret) {
		t.Fatalf("set leaked the key:\n%s", resp.Content)
	}

	dm := config.Get().DecisionModel
	if dm.Router.Model != "tev1:0.8b" || dm.TimeoutMs != 2500 || dm.Router.APIKey != dmSecret {
		t.Fatalf("not applied: %+v", dm)
	}

	// The key is encrypted at rest.
	var found bool
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "tev1:0.8b") {
				found = true
				if strings.Contains(string(b), dmSecret) {
					t.Fatalf("%s stores the key in plaintext", p)
				}
			}
			return nil
		})
	}
	if !found {
		t.Fatal("decision model was not persisted to the project config")
	}

	show := dmRun(t, "")
	if show.IsError {
		t.Fatalf("show failed: %s", show.Content)
	}
	for _, want := range []string{"provider:  ollama", "model:     tev1:0.8b", "timeout:   2500 ms", "••••9876", "consumers: none enabled", "health:    healthy"} {
		if !strings.Contains(show.Content, want) {
			t.Fatalf("show missing %q:\n%s", want, show.Content)
		}
	}
	if strings.Contains(show.Content, dmSecret) {
		t.Fatalf("show leaked the key:\n%s", show.Content)
	}

	// A second set without --api-key keeps the stored key.
	if r := dmRun(t, "set --model tev1:0.8b --keep-alive 10m"); r.IsError {
		t.Fatalf("set failed: %s", r.Content)
	}
	if got := config.Get().DecisionModel.Router.APIKey; got != dmSecret {
		t.Fatalf("stored key was lost: %q", got)
	}

	test := dmRun(t, "test")
	if test.IsError || !strings.Contains(test.Content, "healthy") {
		t.Fatalf("test verdict: %s", test.Content)
	}
}

func TestPandoSetupDecisionModelTestReportsUnhealthy(t *testing.T) {
	withDecisionModelConfig(t)
	srv := systemonetest.New(t) // only tev1:0.8b is installed
	if r := dmRun(t, "set --base-url "+srv.BaseURL()+" --model missing-model"); r.IsError {
		t.Fatalf("set failed: %s", r.Content)
	}
	out := dmRun(t, "test").Content
	if !strings.Contains(out, "NOT healthy") {
		t.Fatalf("expected an unhealthy verdict:\n%s", out)
	}
}

func TestPandoSetupDecisionModelModels(t *testing.T) {
	withDecisionModelConfig(t)
	srv := systemonetest.New(t)
	if r := dmRun(t, "set --base-url "+srv.BaseURL()+" --model tev1:0.8b"); r.IsError {
		t.Fatalf("set failed: %s", r.Content)
	}
	resp := dmRun(t, "models")
	if resp.IsError || !strings.Contains(resp.Content, "tev1:0.8b") {
		t.Fatalf("models: %s", resp.Content)
	}
}

func TestPandoSetupDecisionModelClearKey(t *testing.T) {
	withDecisionModelConfig(t)
	if r := dmRun(t, "set --provider custom --base-url http://127.0.0.1:1 --model jev-latest --api-key "+dmSecret); r.IsError {
		t.Fatalf("set failed: %s", r.Content)
	}
	if r := dmRun(t, "clear-key"); r.IsError {
		t.Fatalf("clear-key failed: %s", r.Content)
	}
	if got := config.Get().DecisionModel.Router.APIKey; got != "" {
		t.Fatalf("key not cleared: %q", got)
	}
}

func TestPandoSetupDecisionModelValidationAndUsageErrors(t *testing.T) {
	withDecisionModelConfig(t)
	for name, args := range map[string]string{
		"bad provider":   "set --provider nope",
		"custom no URL":  "set --provider custom --model x",
		"bad timeout":    "set --timeout-ms abc",
		"nothing to set": "set",
		"unknown action": "frobnicate",
		"empty api key":  "set --api-key",
	} {
		resp := dmRun(t, args)
		if !resp.IsError {
			t.Fatalf("%s: expected an error, got %s", name, resp.Content)
		}
		if strings.Contains(resp.Content, dmSecret) {
			t.Fatalf("%s: leaked secret", name)
		}
	}
	if got := config.Get().DecisionModel.Router.Provider; got == "nope" {
		t.Fatal("invalid provider was saved")
	}
}

func TestPandoSetupDecisionModelNeedsLoadedConfig(t *testing.T) {
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if resp := dmRun(t, "show"); !resp.IsError || !strings.Contains(resp.Content, "not loaded") {
		t.Fatalf("expected a config-not-loaded error, got %q", resp.Content)
	}
}
