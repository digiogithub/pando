package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/history"
)

type fakeHistory struct {
	history.Service
	files []history.File
}

func (f fakeHistory) ListBySession(_ context.Context, _ string) ([]history.File, error) {
	return f.files, nil
}

func TestHandleSessionFileOriginal(t *testing.T) {
	hist := fakeHistory{files: []history.File{
		{Path: "/repo/other.go", Version: history.InitialVersion, Content: "other"},
		{Path: "/repo/pkg/a.go", Version: history.InitialVersion, Content: "before"},
		{Path: "/repo/pkg/a.go", Version: "v1", Content: "after"},
	}}
	s := &Server{app: &app.App{History: hist}}

	cases := []struct {
		path      string
		wantFound bool
		want      string
	}{
		{"/repo/pkg/a.go", true, "before"},
		{"pkg/a.go", true, "before"},
		{"./pkg/a.go", true, "before"},
		{"a.go", true, "before"},
		{"/elsewhere/pkg/a.go", false, ""},
		{"missing.go", false, ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s1/file-original?path="+tc.path, nil)
		req.SetPathValue("id", "s1")
		rec := httptest.NewRecorder()
		s.handleSessionFileOriginal(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, rec.Code)
		}
		var body struct {
			Found    bool   `json:"found"`
			Original string `json:"original"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", tc.path, err)
		}
		if body.Found != tc.wantFound || body.Original != tc.want {
			t.Errorf("%s: got found=%v original=%q, want found=%v original=%q",
				tc.path, body.Found, body.Original, tc.wantFound, tc.want)
		}
	}
}
