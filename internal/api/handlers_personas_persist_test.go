package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/spf13/viper"
)

func TestSetActivePersonaPersistsAcrossRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	_ = config.Reload()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
	if _, err := config.Load(dir, false); err != nil {
		t.Fatal(err)
	}

	// "" (Auto) needs no persona manager, so it exercises persistence alone.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/personas/active", strings.NewReader(`{"name":""}`))
	rec := httptest.NewRecorder()
	(&Server{}).handleSetActivePersona(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	_ = config.Reload()
	viper.Reset()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, auto, set := config.ActivePersonaChoice(); !auto || !set {
		t.Fatalf("auto=%v set=%v after restart", auto, set)
	}
}

func TestGetActivePersonaReportsAutoFields(t *testing.T) {
	resetProviderAccountsTestConfig(t)
	cfg := config.Get()
	cfg.PersonaAutoSelect.Enabled = true
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentPersonaSelector: {UseDecisionModel: true}}

	rec := httptest.NewRecorder()
	(&Server{}).handleGetActivePersona(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas/active?sessionId=nope", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["active"] != "" || got["auto"] != true || got["decisionModel"] != true || got["applied"] != "" || got["source"] != "" {
		t.Fatalf("response = %v", got)
	}

	cfg.PersonaAutoSelect.Enabled = false
	rec = httptest.NewRecorder()
	(&Server{}).handleGetActivePersona(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas/active", nil))
	got = map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["auto"] != false || got["active"] != "" {
		t.Fatalf("auto-select off response = %v", got)
	}
}
