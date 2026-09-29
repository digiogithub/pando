package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func TestPutConfigLSPKeepsAutostartAndOptions(t *testing.T) {
	resetProviderAccountsTestConfig(t)
	cfg := config.Get()
	cfg.LSP["go"] = config.LSPConfig{Command: "gopls", Options: map[string]any{"a": 1}}

	s := &Server{}
	body := `{"language":"go","command":"gopls","args":[],"languages":[".go"],"autostart":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/lsp", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	s.handlePutConfigLSP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		LSP []LSPConfigItem `json:"lsp"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	found := false
	for _, item := range resp.LSP {
		if item.Language == "go" {
			found = true
			if !item.Autostart || len(item.Languages) != 1 {
				t.Fatalf("item = %+v, want autostart and languages kept", item)
			}
		}
	}
	if !found {
		t.Fatalf("go LSP missing from response: %s", rec.Body.String())
	}
	if config.Get().LSP["go"].Options == nil {
		t.Fatal("existing Options were wiped by the PUT")
	}
}
