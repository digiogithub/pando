package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEvaluatorConfigJSONKeysMatchWebUI guards the /api/v1/config/evaluator
// contract: the WebUI reads and writes camelCase keys. Without json tags Go
// emitted "Enabled"/"Model", the WebUI showed the section as unconfigured, and a
// save sent both spellings so the stale capitalised value won on decode.
func TestEvaluatorConfigJSONKeysMatchWebUI(t *testing.T) {
	raw, err := json.Marshal(EvaluatorConfig{Enabled: true, Model: "copilot.gpt-6-luna", Provider: "copilot"})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"enabled", "model", "provider", "alphaWeight", "betaWeight", "explorationC",
		"minSessionsForUCB", "correctionsPatterns", "maxTokensBaseline", "maxSkills", "judgePromptTemplate", "async"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing camelCase key %q in %s", k, raw)
		}
	}
	for k := range keys {
		if k[:1] == strings.ToUpper(k[:1]) {
			t.Errorf("capitalised key %q leaks into the API", k)
		}
	}

	var decoded EvaluatorConfig
	if err := json.Unmarshal([]byte(`{"enabled":true,"model":"copilot.gpt-6-luna","provider":"copilot"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Enabled || decoded.Model != "copilot.gpt-6-luna" || decoded.Provider != "copilot" {
		t.Fatalf("decoded = %+v", decoded)
	}
}
