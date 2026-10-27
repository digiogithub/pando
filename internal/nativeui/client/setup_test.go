package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupEndpoints(t *testing.T) {
	var gotScope, gotModels map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"workingDir":"/w","needed":true,"hasLocalConfig":false}`))
	})
	mux.HandleFunc("/api/v1/setup/scope", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotScope)
		_, _ = w.Write([]byte(`{"scope":"global","configPath":"/c","status":{"needed":false}}`))
	})
	mux.HandleFunc("/api/v1/setup/suggested-models", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("provider") != "open ai" || r.URL.Query().Get("refresh") != "1" {
			t.Errorf("query %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"mainModel":"a","fastModel":"b"}`))
	})
	mux.HandleFunc("/api/v1/setup/models", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotModels)
		_, _ = w.Write([]byte(`{"mainModel":"a","fastModel":"a"}`))
	})
	mux.HandleFunc("/api/v1/config/provider-accounts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"providerAccounts":[{"id":"x","type":"openai"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})
	ctx := context.Background()

	st, err := c.SetupStatus(ctx)
	if err != nil || !st.Needed || st.WorkingDir != "/w" {
		t.Fatalf("status %+v %v", st, err)
	}
	r, err := c.SetupScope(ctx, "global")
	if err != nil || r.ConfigPath != "/c" || gotScope["scope"] != "global" {
		t.Fatalf("scope %+v %v %v", r, err, gotScope)
	}
	sm, err := c.SetupSuggestedModels(ctx, "open ai", true)
	if err != nil || sm.MainModel != "a" {
		t.Fatalf("suggest %+v %v", sm, err)
	}
	if _, err := c.SetupSaveModels(ctx, "a", ""); err != nil || gotModels["mainModel"] != "a" {
		t.Fatalf("models %v %v", gotModels, err)
	}
	accs, err := c.SetupProviderAccounts(ctx)
	if err != nil || len(accs) != 1 || accs[0].Type != "openai" {
		t.Fatalf("accounts %+v %v", accs, err)
	}
}
