package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleExtensionsHostServesContractPage(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleExtensionsHost(rec, httptest.NewRequest(http.MethodGet, "/api/v1/extensions/host?panel=ui.acme.reports", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"window.__PANDO_UI__", "/api/v1/extensions/ui", "extensionId: panel.extension", "VERSION = 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("host page lacks %q", want)
		}
	}
	// The panel id must come from the URL at run time, never be baked into
	// the markup.
	if strings.Contains(body, "ui.acme.reports") {
		t.Error("panel id leaked into the markup")
	}
}

func TestHandleExtensionsHostRejectsNonGET(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleExtensionsHost(rec, httptest.NewRequest(http.MethodPost, "/api/v1/extensions/host", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
