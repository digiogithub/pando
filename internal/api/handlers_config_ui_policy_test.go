package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/extensions"
)

// The UI policy is what a settings surface reads before it draws anything, so
// the endpoint has to answer with the live policy and with empty lists (never
// null) when no extension declares one.
func TestConfigUIPolicyEndpoint(t *testing.T) {
	server := &Server{}
	t.Cleanup(func() { extensions.SetUIPolicyResolver(nil) })

	// Nothing declared: empty lists, so a client can iterate them blindly.
	got := uiPolicyFromAPI(t, server)
	if !reflect.DeepEqual(got.HiddenSections, []string{}) || !reflect.DeepEqual(got.ReadOnlySections, []string{}) {
		t.Fatalf("policy with no extension = %+v, want empty lists", got)
	}
	if got.ReadOnlyLabel != "" || got.Banner.Text != "" || got.Banner.Link != "" {
		t.Fatalf("policy with no extension = %+v, want no label and no banner", got)
	}

	extensions.SetUIPolicyResolver(func(context.Context) extensions.UIPolicy {
		return extensions.UIPolicy{
			HiddenSections:   []string{"providerAccounts", "internalTools"},
			ReadOnlySections: []string{"tui.theme"},
			ReadOnlyLabel:    "Managed by the operator",
			Banner: extensions.UIPolicyBanner{
				Text: "This device is managed",
				Link: "https://example.test/settings",
			},
		}
	})

	got = uiPolicyFromAPI(t, server)
	wantHidden := []string{"providerAccounts", "internalTools"}
	if !reflect.DeepEqual(got.HiddenSections, wantHidden) {
		t.Fatalf("hiddenSections = %v, want %v", got.HiddenSections, wantHidden)
	}
	if !reflect.DeepEqual(got.ReadOnlySections, []string{"tui.theme"}) {
		t.Fatalf("readOnlySections = %v", got.ReadOnlySections)
	}
	if got.ReadOnlyLabel != "Managed by the operator" {
		t.Fatalf("readOnlyLabel = %q", got.ReadOnlyLabel)
	}
	if got.Banner.Text != "This device is managed" || got.Banner.Link != "https://example.test/settings" {
		t.Fatalf("banner = %+v", got.Banner)
	}
}

func TestConfigUIPolicyRejectsNonGet(t *testing.T) {
	server := &Server{}
	rec := httptest.NewRecorder()
	server.handleConfigUIPolicy(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/ui-policy", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func uiPolicyFromAPI(t *testing.T, server *Server) UIPolicyResponse {
	t.Helper()
	// The process-wide policy is memoised, so a test that changes it has to say
	// so before reading it back.
	extensions.InvalidateUIPolicy()

	rec := httptest.NewRecorder()
	server.handleConfigUIPolicy(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/ui-policy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body UIPolicyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HiddenSections == nil || body.ReadOnlySections == nil {
		t.Fatalf("a section list came back null, want a list: %s", rec.Body.String())
	}
	return body
}
