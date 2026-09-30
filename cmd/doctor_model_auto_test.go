package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func doctorCfg(baseURL, model string, route models.ModelID) *config.Config {
	return &config.Config{
		Providers: map[models.ModelProvider]config.Provider{models.ProviderAnthropic: {APIKey: "k"}},
		ModelAutoMode: config.ModelAutoModeConfig{
			Enabled:   true,
			Threshold: 0.6,
			Router:    config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: baseURL, Model: model},
			Routes:    []config.ModelAutoRoute{{ID: "code", Description: "coding", Model: route}},
		},
	}
}

func runDoctor(t *testing.T, cfg *config.Config) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	n := doctorModelAutoMode(context.Background(), cfg, &buf)
	return buf.String(), n
}

func TestDoctorModelAutoMode(t *testing.T) {
	good := systemonetest.NewOllama035(t)

	// Disabled: nothing to check.
	if out, n := runDoctor(t, &config.Config{}); n != 0 || !strings.Contains(out, "disabled") {
		t.Fatalf("disabled: n=%d out=%s", n, out)
	}

	// Healthy: Ollama 0.35 + decision model + a known route model.
	out, n := runDoctor(t, doctorCfg(good.URL, "tev1:0.8b", models.Claude35Haiku))
	if n != 0 {
		t.Fatalf("healthy setup reported %d problems:\n%s", n, out)
	}
	for _, want := range []string{"reachable", "0.35", "decision-capable", "route \"code\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// Old Ollama.
	old := systemonetest.NewOllama035(t, systemonetest.WithVersion("0.34.1"))
	out, n = runDoctor(t, doctorCfg(old.URL, "tev1:0.8b", models.Claude35Haiku))
	if n == 0 || !strings.Contains(out, "0.35") || !strings.Contains(out, "upgrade") {
		t.Fatalf("old ollama: n=%d\n%s", n, out)
	}

	// Model not installed: pull hint.
	out, n = runDoctor(t, doctorCfg(good.URL, "absent:1b", models.Claude35Haiku))
	if n == 0 || !strings.Contains(out, "ollama pull") {
		t.Fatalf("missing model: n=%d\n%s", n, out)
	}

	// Unreachable.
	out, n = runDoctor(t, doctorCfg("http://127.0.0.1:1", "tev1:0.8b", models.Claude35Haiku))
	if n == 0 || !strings.Contains(out, "not reachable") || !strings.Contains(out, "ollama serve") {
		t.Fatalf("unreachable: n=%d\n%s", n, out)
	}

	// Unknown route model.
	out, n = runDoctor(t, doctorCfg(good.URL, "tev1:0.8b", models.ModelID("nope.not-a-model")))
	if n == 0 || !strings.Contains(out, "not a known model") {
		t.Fatalf("unknown route model: n=%d\n%s", n, out)
	}

	// Remote provider with a wrong key.
	remote := systemonetest.NewRemote(t, systemonetest.WithAPIKey("right"))
	cfg := doctorCfg(remote.URL, "jev-latest", models.Claude35Haiku)
	cfg.ModelAutoMode.Router.Provider = config.DecisionProviderCustom
	cfg.ModelAutoMode.Router.APIKey = "wrong"
	out, n = runDoctor(t, cfg)
	if n == 0 || !strings.Contains(out, "credentials") || strings.Contains(out, "wrong") {
		t.Fatalf("remote unauthorized: n=%d\n%s", n, out)
	}
}
