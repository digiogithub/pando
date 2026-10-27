package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetMesnadaEnginesReportsNativeLockedAndDisabled(t *testing.T) {
	server, _ := withCavemanSettings(t, "")
	putSettings(t, server, `{"orchestrator_disabled_engines":["claude","pando"]}`)

	rec := httptest.NewRecorder()
	server.handleGetMesnadaEngines(rec, httptest.NewRequest(http.MethodGet, "/api/v1/mesnada/engines", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var resp MesnadaEnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	by := map[string]bool{}
	locked := map[string]bool{}
	for _, e := range resp.Engines {
		by[e.Name] = e.Enabled
		locked[e.Name] = e.Locked
	}
	if !by["pando"] || !locked["pando"] || !locked["warm-acp"] {
		t.Fatalf("native engines must be enabled and locked: %+v", resp.Engines)
	}
	if by["claude"] || locked["claude"] {
		t.Fatalf("claude should be disabled and toggleable: %+v", resp.Engines)
	}
	if !by["copilot"] {
		t.Fatalf("copilot should stay enabled: %+v", resp.Engines)
	}
}

func TestSettingsDisabledEnginesAndNestedToggleRoundTrip(t *testing.T) {
	server, _ := withCavemanSettings(t, "")

	get := func() SettingsResponse {
		rec := httptest.NewRecorder()
		server.handleSettings(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
		return decodeSettings(t, rec)
	}
	if s := get(); !s.DelegationAllowNestedSubagents || len(s.OrchestratorDisabledEngines) != 0 || s.OrchestratorDisabledEngines == nil {
		t.Fatalf("defaults: nested=%v disabled=%v (want true, [])", s.DelegationAllowNestedSubagents, s.OrchestratorDisabledEngines)
	}

	rec := putSettings(t, server, `{"orchestrator_disabled_engines":["Copilot","warm-acp","gemini"],"delegation_allow_nested_subagents":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", rec.Code, rec.Body)
	}
	s := decodeSettings(t, rec)
	if !reflect.DeepEqual(s.OrchestratorDisabledEngines, []string{"copilot", "gemini"}) {
		t.Fatalf("disabled engines = %v, want [copilot gemini]", s.OrchestratorDisabledEngines)
	}
	if s.DelegationAllowNestedSubagents {
		t.Fatal("nested toggle not persisted")
	}

	if rec := putSettings(t, server, `{"delegation_max_depth":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("delegation_max_depth=0 should be rejected, got %d", rec.Code)
	}
}
