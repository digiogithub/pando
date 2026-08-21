package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/extensions"
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

// The memory indicator must be able to ask any build, including one without
// the capability compiled in. Answering 404 there would make "no such feature"
// and "feature switched off" the same unknown to the UI, and the indicator
// would have to guess which one it is looking at.
func TestHandleExtensionsMemoryOnStandardBuild(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()

	s.handleExtensionsMemory(rec, httptest.NewRequest(http.MethodGet, "/api/v1/extensions/memory", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got extensions.MemoryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.Enabled || got.Active {
		t.Fatalf("status = %+v", got)
	}
	if got.Sinks == nil {
		t.Fatalf("sinks is null, want an empty list: %s", rec.Body.String())
	}
}

func TestHandleExtensionsMemoryRejectsNonGET(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()

	s.handleExtensionsMemory(rec, httptest.NewRequest(http.MethodPost, "/api/v1/extensions/memory", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
