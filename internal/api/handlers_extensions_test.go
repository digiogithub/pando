package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A standard build has no frontend extensions, and the shell must still get a
// well-formed manifest with an empty list rather than a null it has to guard.
func TestHandleExtensionsUIEmptyManifest(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()

	s.handleExtensionsUI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/extensions/ui", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got UIManifestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.Panels == nil {
		t.Fatalf("panels is null, want an empty list: %s", rec.Body.String())
	}
	if len(got.Panels) != 0 {
		t.Fatalf("panels = %+v", got.Panels)
	}
}

func TestHandleExtensionsUIRejectsNonGET(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()

	s.handleExtensionsUI(rec, httptest.NewRequest(http.MethodPost, "/api/v1/extensions/ui", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
