package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/design"
)

// The Settings panel drives the design system entirely over HTTP, so the loop
// it performs is the contract: read what the project has, extract a system from
// a bundled guide, edit a token, and link the result into an artifact.
func TestDesignSystemHTTPLoop(t *testing.T) {
	s, artifact := designStudioServer(t)

	var initial DesignSystemResponse
	if code := call(t, s, http.MethodGet, "/api/v1/design/system", "", &initial); code != http.StatusOK {
		t.Fatalf("GET system = %d", code)
	}
	if initial.Exists {
		t.Error("a fresh project must report that it has not committed a system")
	}
	for _, path := range []string{initial.Tokens, initial.Stylesheet, initial.Contract} {
		if !strings.HasPrefix(path, "designer/_system/") {
			t.Errorf("path %q does not sit in the design system directory", path)
		}
	}

	var examples struct {
		Examples []struct{ Name, Title string } `json:"examples"`
	}
	if code := call(t, s, http.MethodGet, "/api/v1/design/system/examples", "", &examples); code != http.StatusOK {
		t.Fatalf("GET examples = %d", code)
	}
	if len(examples.Examples) == 0 {
		t.Fatal("no bundled guides were listed")
	}
	for _, e := range examples.Examples {
		if e.Name == "" || e.Title == "" {
			t.Errorf("bundled guide %+v is missing a name or a title", e)
		}
	}

	// A dry run must show what would happen and change nothing on disk.
	var dry struct {
		Result design.ExtractResult `json:"result"`
		Saved  bool                 `json:"saved"`
	}
	body := `{"source":"text","target":"` + examples.Examples[0].Name + `","dry_run":true}`
	if code := call(t, s, http.MethodPost, "/api/v1/design/system/extract", body, &dry); code != http.StatusOK {
		t.Fatalf("dry-run extract = %d", code)
	}
	if dry.Saved {
		t.Error("a dry run reported itself as saved")
	}
	var stillMissing DesignSystemResponse
	call(t, s, http.MethodGet, "/api/v1/design/system", "", &stillMissing)
	if stillMissing.Exists {
		t.Error("a dry run wrote the design system")
	}

	var saved struct {
		Saved  bool                 `json:"saved"`
		System DesignSystemResponse `json:"system"`
	}
	body = `{"source":"text","target":"` + examples.Examples[0].Name + `","name":"House"}`
	if code := call(t, s, http.MethodPost, "/api/v1/design/system/extract", body, &saved); code != http.StatusOK {
		t.Fatalf("extract = %d", code)
	}
	if !saved.Saved || !saved.System.Exists {
		t.Fatalf("extraction did not persist: %+v", saved)
	}
	if saved.System.System.Name != "House" {
		t.Errorf("system name = %q, want the requested House", saved.System.System.Name)
	}

	var updated DesignSystemResponse
	if code := call(t, s, http.MethodPut, "/api/v1/design/system",
		`{"tokens":{"color":{"accent":"#123456"}}}`, &updated); code != http.StatusOK {
		t.Fatalf("PUT system = %d", code)
	}
	if updated.System.Tokens["color"]["accent"] != "#123456" {
		t.Errorf("accent = %q, want #123456", updated.System.Tokens["color"]["accent"])
	}
	if updated.System.Name != "House" {
		t.Errorf("an edit that sends no name renamed the system to %q", updated.System.Name)
	}

	var applied design.ApplyResult
	if code := call(t, s, http.MethodPost,
		"/api/v1/design/artifacts/"+artifact.ID+"/apply-system", "", &applied); code != http.StatusOK {
		t.Fatalf("apply-system = %d", code)
	}
	if !applied.Linked || applied.Stylesheet == "" {
		t.Errorf("apply did not link the stylesheet: %+v", applied)
	}
}

// An extraction target the server cannot read is a client error, not a 500: the
// user typed a path or a guide name that does not exist.
func TestDesignSystemExtractRejectsAnUnknownTarget(t *testing.T) {
	s, _ := designStudioServer(t)
	if code := call[struct{}](t, s, http.MethodPost, "/api/v1/design/system/extract",
		`{"source":"text","target":"not-a-guide"}`, nil); code != http.StatusBadRequest {
		t.Errorf("extract from an unknown guide = %d, want 400", code)
	}
}
