package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The chat mode must round-trip through the user-level file so a restart on a
// different port (another localStorage origin) still opens the chosen view.
func TestUIPrefsEndpointRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := &Server{}

	get := func() UIPrefs {
		t.Helper()
		rec := httptest.NewRecorder()
		server.handleUIPrefs(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ui/preferences", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status = %d, body %s", rec.Code, rec.Body.String())
		}
		var p UIPrefs
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return p
	}
	put := func(body string) int {
		rec := httptest.NewRecorder()
		server.handleUIPrefs(rec, httptest.NewRequest(http.MethodPut, "/api/v1/ui/preferences", strings.NewReader(body)))
		return rec.Code
	}

	if got := get(); got.ChatMode != "" {
		t.Fatalf("fresh prefs chatMode = %q, want empty", got.ChatMode)
	}
	if code := put(`{"chatMode":"simple"}`); code != http.StatusOK {
		t.Fatalf("PUT simple status = %d", code)
	}
	if got := get(); got.ChatMode != "simple" {
		t.Fatalf("chatMode = %q, want simple", got.ChatMode)
	}
	// An empty patch leaves the stored value alone.
	if code := put(`{}`); code != http.StatusOK {
		t.Fatalf("PUT empty status = %d", code)
	}
	if got := get(); got.ChatMode != "simple" {
		t.Fatalf("chatMode after empty patch = %q, want simple", got.ChatMode)
	}
	if code := put(`{"chatMode":"weird"}`); code != http.StatusBadRequest {
		t.Fatalf("PUT invalid status = %d, want 400", code)
	}
	if code := put(`{"chatMode":"advanced"}`); code != http.StatusOK {
		t.Fatalf("PUT advanced status = %d", code)
	}
	if got := get(); got.ChatMode != "advanced" {
		t.Fatalf("chatMode = %q, want advanced", got.ChatMode)
	}
}
