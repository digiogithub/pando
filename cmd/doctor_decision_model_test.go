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

func runDecisionDoctor(t *testing.T, cfg *config.Config) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	n := doctorDecisionModel(context.Background(), cfg, &buf)
	return buf.String(), n
}

func TestDoctorDecisionModelBlock(t *testing.T) {
	good := systemonetest.NewOllama035(t)

	// No consumer: described but not probed, no problems.
	cfg := &config.Config{DecisionModel: config.DecisionModelConfig{
		Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: "http://127.0.0.1:1", Model: "tev1:0.8b"},
	}}
	out, n := runDecisionDoctor(t, cfg)
	if n != 0 || !strings.Contains(out, "not used") || strings.Contains(out, "not reachable") {
		t.Fatalf("no consumer: n=%d\n%s", n, out)
	}

	// Healthy, every consumer on: full snapshot of the block.
	cfg = doctorCfg(good.URL, "tev1:0.8b", models.Claude35Haiku)
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentPersonaSelector: {UseDecisionModel: true}}
	cfg.Remembrances.ContextEnrichmentDecisionFilterEnabled = true
	cfg.Remembrances.MemoryContextDecisionFilterEnabled = true
	out, n = runDecisionDoctor(t, cfg)
	if n != 0 {
		t.Fatalf("healthy: n=%d\n%s", n, out)
	}
	for _, want := range []string{
		"Decision model\n",
		"provider: ollama",
		"base URL: " + good.URL,
		`model:    "tev1:0.8b"`,
		"timeout:  1500 ms",
		"model auto mode:  on",
		"persona selector: on",
		"context filter:   on (threshold 0.60, max candidates 32, local only)",
		"memory filter:    on (threshold 0.60, max candidates 32, local only)",
		"reachable", "decision-capable",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// Context filter alone is a consumer; no model is a problem.
	cfg = &config.Config{}
	cfg.Remembrances.ContextEnrichmentDecisionFilterEnabled = true
	out, n = runDecisionDoctor(t, cfg)
	if n == 0 || !strings.Contains(out, "no decision model configured") || !strings.Contains(out, "context filter:   on") {
		t.Fatalf("no model: n=%d\n%s", n, out)
	}

	// Hosted provider with a local-only filter warns.
	remote := systemonetest.NewRemote(t)
	cfg = doctorCfg(remote.URL, "jev-latest", models.Claude35Haiku)
	cfg.ModelAutoMode.Enabled = false
	cfg.DecisionModel.Router.Provider = config.DecisionProviderCustom
	cfg.Remembrances.ContextEnrichmentDecisionFilterEnabled = true
	out, _ = runDecisionDoctor(t, cfg)
	if !strings.Contains(out, "local-only") || !strings.Contains(out, "filter is skipped") {
		t.Fatalf("hosted warning missing:\n%s", out)
	}
}

func TestDoctorChecksReferToDecisionBlock(t *testing.T) {
	good := systemonetest.NewOllama035(t)
	cfg := doctorCfg(good.URL, "tev1:0.8b", models.Claude35Haiku)
	var buf bytes.Buffer
	doctorModelAutoMode(context.Background(), cfg, &buf)
	if out := buf.String(); strings.Contains(out, "router:") || !strings.Contains(out, "see the Decision model block") {
		t.Fatalf("model auto check still prints router lines:\n%s", out)
	}
}
