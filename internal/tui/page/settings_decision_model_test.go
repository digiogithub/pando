package page

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

func decisionFieldByKey(cfg *config.Config, key string) (settings.Field, bool) {
	for _, f := range buildDecisionModelSection(cfg).Fields {
		if f.Key == key {
			return f, true
		}
	}
	return settings.Field{}, false
}

func TestDecisionModelSectionFieldsPerProvider(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	t.Cleanup(func() { setDecisionDiscovery("", nil); setDecisionModels(nil) })

	if title := buildDecisionModelSection(cfg).Title; title != "Decision model" {
		t.Errorf("title = %q", title)
	}
	for _, key := range []string{
		"decisionModel.router.provider", "decisionModel.router.baseURL", "decisionModel.router.apiKey",
		"decisionModel.router.model", "decisionModel.router.keepAlive", "decisionModel.timeoutMs",
		"decisionModel.info.url", "decisionModel.info.health",
		"action:decision_model_discover", "action:decision_model_test",
	} {
		if _, ok := decisionFieldByKey(cfg, key); !ok {
			t.Errorf("missing ollama field %q", key)
		}
	}
	for _, key := range []string{"decisionModel.router.preset", "decisionModel.router.headers", "decisionModel.info.privacy"} {
		if _, ok := decisionFieldByKey(cfg, key); ok {
			t.Errorf("ollama must not expose %q", key)
		}
	}
	if f, _ := decisionFieldByKey(cfg, "decisionModel.router.baseURL"); !f.Disabled {
		t.Error("Ollama base URL must be read-only")
	}

	// Custom: preset, headers, privacy note, no keep-alive.
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.provider", Value: "custom"}); err != nil {
		t.Fatal(err)
	}
	cfg = config.Get()
	for _, key := range []string{"decisionModel.router.preset", "decisionModel.router.headers", "decisionModel.info.privacy"} {
		if _, ok := decisionFieldByKey(cfg, key); !ok {
			t.Errorf("custom must expose %q", key)
		}
	}
	if _, ok := decisionFieldByKey(cfg, "decisionModel.router.keepAlive"); ok {
		t.Error("keep-alive is Ollama-only")
	}
	preset, _ := decisionFieldByKey(cfg, "decisionModel.router.preset")
	if strings.Join(preset.Options, ",") != "(choose),OpenRouter,LiteLLM,Kev" {
		t.Errorf("presets = %v", preset.Options)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.preset", Value: "OpenRouter"}); err != nil {
		t.Fatal(err)
	}
	r := config.Get().DecisionModel.Router
	if r.BaseURL != "https://openrouter.ai/api" || r.Model != "typesafe/jev-1.13" {
		t.Fatalf("openrouter preset: %+v", r)
	}
	prov, _ := decisionFieldByKey(config.Get(), "decisionModel.router.provider")
	if !strings.Contains(prov.Hint, "openrouter.ai") {
		t.Errorf("privacy hint = %q", prov.Hint)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.preset", Value: "Kev"}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().DecisionModel.Router.BaseURL; got != "http://localhost:8009" {
		t.Errorf("kev preset url = %q", got)
	}
}

func TestDecisionModelSaveKeyHeadersAndTimeout(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)

	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.apiKey", Value: "sk-live-9876"}); err != nil {
		t.Fatalf("set key: %v", err)
	}
	cfg = config.Get()
	keyField, _ := decisionFieldByKey(cfg, "decisionModel.router.apiKey")
	if strings.Contains(keyField.Value, "sk-live-9876") || !strings.HasSuffix(keyField.Value, "9876") {
		t.Errorf("key field must be masked, got %q", keyField.Value)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.apiKey", Value: keyField.Value}); err != nil {
		t.Fatalf("masked resave: %v", err)
	}
	if config.Get().DecisionModel.Router.APIKey == "" {
		t.Error("masked resave must keep the stored key")
	}
	if _, ok := decisionFieldByKey(config.Get(), "action:decision_model_clear_key"); !ok {
		t.Fatal("clear action missing while a key is stored")
	}
	if err := config.ClearDecisionModelAPIKey(); err != nil {
		t.Fatal(err)
	}
	if _, ok := decisionFieldByKey(config.Get(), "action:decision_model_clear_key"); ok {
		t.Error("clear action must disappear once the key is cleared")
	}

	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.keepAlive", Value: "10m"}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().DecisionModel.Router.KeepAlive; got != "10m" {
		t.Errorf("keepAlive = %q", got)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.timeoutMs", Label: "Timeout", Value: "2500"}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().DecisionModel.TimeoutMs; got != 2500 {
		t.Errorf("timeout = %d", got)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.timeoutMs", Label: "Timeout", Value: "abc"}); err == nil {
		t.Error("non-numeric timeout must fail")
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.bogus", Value: "x"}); err == nil {
		t.Error("unknown key must fail")
	}

	// Headers: values stay masked on screen and survive a masked resave.
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.provider", Value: "custom"}); err != nil {
		t.Fatal(err)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.headers", Value: "X-Team: alpha; X-Env: prod"}); err != nil {
		t.Fatal(err)
	}
	h, _ := decisionFieldByKey(config.Get(), "decisionModel.router.headers")
	if strings.Contains(h.Value, "alpha") || !strings.Contains(h.Value, "X-Team") {
		t.Errorf("headers must be masked, got %q", h.Value)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.headers", Value: h.Value}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().DecisionModel.Router.Headers["X-Team"]; got != "alpha" {
		t.Errorf("masked resave lost the header value: %q", got)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.headers", Value: "nonsense"}); err == nil {
		t.Error("malformed header must fail")
	}
}

func TestDecisionModelValidationAndConsumerGuard(t *testing.T) {
	withModelAutoTUIConfig(t)

	// Invalid values surface the config validation error and are not stored.
	err := saveDecisionModel(settings.Field{Key: "decisionModel.timeoutMs", Label: "Timeout", Value: "-5"})
	if err == nil {
		t.Fatal("negative timeout must be rejected")
	}
	if got := config.Get().DecisionModel.TimeoutMs; got != 0 {
		t.Errorf("invalid save leaked: %d", got)
	}

	// With a consumer enabled the model cannot be emptied or switched away.
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.enabled", Label: "Enabled", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	err = saveDecisionModel(settings.Field{Key: "decisionModel.router.model", Value: ""})
	if err == nil || !strings.Contains(err.Error(), "uses the decision model") {
		t.Errorf("empty model with consumer enabled = %v", err)
	}
	if err := saveDecisionModel(settings.Field{Key: "decisionModel.router.provider", Value: "typesafe"}); err == nil {
		t.Error("switching provider (clears the model) while a feature is enabled must fail")
	}
	if got := config.Get().DecisionModel.Router.Model; got != "tev1:0.8b" {
		t.Errorf("model = %q after refused changes", got)
	}
}

func TestDecisionModelDiscoveryAndPullActions(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	t.Cleanup(func() { setDecisionDiscovery("", nil); setDecisionModels(nil) })
	fake := systemonetest.NewOllama035(t)
	cfg.DecisionModel.Router.BaseURL = fake.URL

	// Discovery result feeds the model select.
	setDecisionModels([]string{"tev1:0.8b", "other"})
	model, _ := decisionFieldByKey(cfg, "decisionModel.router.model")
	if model.Type != settings.FieldSelect || len(model.Options) < 2 {
		t.Errorf("model field = %+v", model)
	}

	// Show-all toggle appears for a filtered listing; free text when unsupported.
	setDecisionDiscovery("filtered", []string{"nimble"})
	if _, ok := decisionFieldByKey(cfg, "action:decision_model_toggle_show_all"); !ok {
		t.Error("show-all action missing for filtered listing")
	}
	if _, ok := decisionFieldByKey(cfg, "action:decision_model_pull:nimble"); !ok {
		t.Fatal("pull action missing for suggestion")
	}
	setDecisionModels(nil)
	setDecisionDiscovery("unsupported", nil)
	if _, ok := decisionFieldByKey(cfg, "action:decision_model_toggle_show_all"); ok {
		t.Error("show-all must be hidden for unsupported listing")
	}
	model, _ = decisionFieldByKey(cfg, "decisionModel.router.model")
	if model.Type != settings.FieldText {
		t.Errorf("model field type = %v, want free text", model.Type)
	}

	// Non-suggested models never reach the server.
	_ = pullDecisionModel("llama3:70b")()
	if fake.Count("/api/pull") != 0 {
		t.Fatal("non-suggested model was pulled")
	}
	var got *decisionPullMsg
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, sub := range m {
				run(sub)
			}
		case decisionPullMsg:
			got = &m
		}
	}
	run(pullDecisionModel("nimble"))
	if got == nil || got.err != nil {
		t.Fatalf("pull result = %+v", got)
	}
	if fake.Count("/api/pull") != 1 {
		t.Fatalf("fake pulls = %d", fake.Count("/api/pull"))
	}

	// Discovery and test connection produce typed messages.
	if msg, ok := discoverDecisionModels()().(decisionDiscoverMsg); !ok || msg.err != nil {
		t.Errorf("discover = %+v ok=%v", msg, ok)
	}
	if msg, ok := testDecisionConnection()().(decisionTestResultMsg); !ok || msg.err != nil || msg.summary == "" {
		t.Errorf("test = %+v ok=%v", msg, ok)
	}
}

func TestDecisionModelInfoRows(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	rows := decisionModelInfoRows(cfg, "x.")
	if len(rows) == 0 || rows[0].Key != "x.routerInfo" || rows[0].Value != "ollama/tev1:0.8b" {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if !r.Disabled {
			t.Errorf("info row %q must be read-only", r.Key)
		}
	}
	cfg.DecisionModel.Router.Model = ""
	rows = decisionModelInfoRows(cfg, "x.")
	if len(rows) != 1 || !strings.Contains(rows[0].Value, "Not configured") {
		t.Fatalf("unconfigured rows = %+v", rows)
	}
}

func TestRemembrancesDecisionFilterFields(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	cfg.Remembrances.Enabled = true
	cfg.Remembrances.ChunkSize = 1000
	cfg.Remembrances.UseSameModel = true
	cfg.Remembrances.DocumentEmbeddingProvider = "ollama"
	cfg.Remembrances.DocumentEmbeddingModel = "nomic-embed-text"
	cfg.Remembrances.CodeEmbeddingProvider = "ollama"
	cfg.Remembrances.CodeEmbeddingModel = "nomic-embed-text"
	find := func(key string) (settings.Field, bool) {
		for _, f := range buildRemembrancesSection(nil, config.Get()).Fields {
			if f.Key == key {
				return f, true
			}
		}
		return settings.Field{}, false
	}
	for _, key := range []string{"remembrances.context_enrichment_decision_filter_enabled", "remembrances.memory_context_decision_filter_enabled"} {
		if _, ok := find(key); !ok {
			t.Fatalf("missing %q", key)
		}
	}
	if _, ok := find("remembrances.context_enrichment_decision_filter_threshold"); ok {
		t.Error("filter details must be hidden while the filter is off")
	}

	save := func(key, value string) error {
		return saveRemembrances(settings.Field{Key: key, Label: key, Value: value})
	}
	if err := save("remembrances.context_enrichment_decision_filter_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	for key, val := range map[string]string{
		"remembrances.context_enrichment_decision_filter_threshold":           "0.7",
		"remembrances.context_enrichment_decision_filter_max_candidates":      "16",
		"remembrances.context_enrichment_decision_filter_max_candidate_chars": "250",
		"remembrances.context_enrichment_decision_filter_allow_hosted":        "true",
		"remembrances.memory_context_decision_filter_enabled":                 "true",
	} {
		if err := save(key, val); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	rem := config.Get().Remembrances
	if !rem.ContextEnrichmentDecisionFilterEnabled || !rem.MemoryContextDecisionFilterEnabled || !rem.ContextEnrichmentDecisionFilterAllowHosted ||
		rem.ContextEnrichmentDecisionFilterThreshold != 0.7 || rem.ContextEnrichmentDecisionFilterMaxCandidates != 16 ||
		rem.ContextEnrichmentDecisionFilterMaxCandidateChars != 250 {
		t.Fatalf("filter settings not persisted: %+v", rem)
	}
	if f, ok := find("remembrances.context_enrichment_decision_filter_threshold"); !ok || f.Value != "0.7" {
		t.Errorf("threshold field = %+v ok=%v", f, ok)
	}
	if _, ok := find("remembrances.filter.routerInfo"); !ok {
		t.Error("decision model info rows missing under the filter")
	}

	// Validation errors come back inline and nothing is stored.
	if err := save("remembrances.context_enrichment_decision_filter_threshold", "3"); err == nil {
		t.Error("threshold 3 must be rejected")
	}
	if err := save("remembrances.context_enrichment_decision_filter_max_candidates", "-1"); err == nil {
		t.Error("negative candidates must be rejected")
	}
	if got := config.Get().Remembrances.ContextEnrichmentDecisionFilterThreshold; got != 0.7 {
		t.Errorf("invalid threshold leaked: %v", got)
	}
}
