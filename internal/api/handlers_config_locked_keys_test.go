package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// The locked-key list is what a settings surface reads to render a field as
// managed instead of discovering it by attempting a save and getting a 409
// back, so the endpoint has to answer with the live list and with an empty
// list (never null) when nothing is locked.
func TestConfigLockedKeysEndpoint(t *testing.T) {
	server := &Server{}

	config.ClearOverlayProviders()
	t.Cleanup(config.ClearOverlayProviders)

	// Nothing registered: an empty list, so a client can iterate it blindly.
	if got := lockedKeysFromAPI(t, server); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("lockedKeys with no overlay = %v, want an empty list", got)
	}

	config.RegisterOverlayProvider(config.OverlayProviderFunc(func(context.Context) (config.Overlay, error) {
		return config.Overlay{Locked: []string{"tui.theme", "internalTools.braveApiKey"}}, nil
	}))
	if err := config.ApplyOverlays(context.Background()); err != nil {
		t.Skipf("configuration could not be loaded in this environment: %v", err)
	}

	want := []string{"internalTools.braveApiKey", "tui.theme"}
	if got := lockedKeysFromAPI(t, server); !reflect.DeepEqual(got, want) {
		t.Fatalf("lockedKeys = %v, want %v", got, want)
	}
}

func TestConfigLockedKeysRejectsNonGet(t *testing.T) {
	server := &Server{}
	rec := httptest.NewRecorder()
	server.handleConfigLockedKeys(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/locked-keys", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func lockedKeysFromAPI(t *testing.T, server *Server) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.handleConfigLockedKeys(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/locked-keys", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		LockedKeys []string `json:"lockedKeys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LockedKeys == nil {
		t.Fatal("lockedKeys is null, want a list")
	}
	return body.LockedKeys
}
