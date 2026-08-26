package api

import (
	"net/http"
	"testing"

	"github.com/digiogithub/pando/internal/design"
)

// The inspector drives the quality gate over HTTP: read what is on record, run
// a pass, read it back. Without a browser only the design-system half of the
// audit runs, which is exactly the degraded path a headless server takes.
func TestDesignCritiqueHTTPLoop(t *testing.T) {
	s, artifact := designStudioServer(t)

	var empty struct {
		Exists   bool                    `json:"exists"`
		Settings design.CritiqueSettings `json:"settings"`
	}
	if code := call(t, s, http.MethodGet,
		"/api/v1/design/artifacts/"+artifact.ID+"/critique", "", &empty); code != http.StatusOK {
		t.Fatalf("GET critique = %d", code)
	}
	if empty.Exists {
		t.Error("a fresh artifact reports a critique nobody ran")
	}
	if empty.Settings.MaxRounds <= 0 || empty.Settings.Policy == "" {
		t.Errorf("the panel was given unusable settings: %+v", empty.Settings)
	}

	var report design.CritiqueReport
	if code := call(t, s, http.MethodPost,
		"/api/v1/design/artifacts/"+artifact.ID+"/critique",
		`{"skip_render":true,"score":6,"summary":"the hero says nothing",`+
			`"issues":[{"severity":"warning","message":"the hero says nothing the reader did not already know"}]}`,
		&report); code != http.StatusOK {
		t.Fatalf("POST critique = %d", code)
	}
	if report.Rendered {
		t.Error("skip_render still rendered")
	}
	if report.RenderError == "" {
		t.Error("a pass that could not render must say so; a silent partial audit reads as a clean one")
	}
	if !report.Recorded {
		t.Error("the pass was not recorded")
	}
	if report.Critique.Score <= 0 || report.Critique.Score > 10 {
		t.Errorf("score out of range: %.1f", report.Critique.Score)
	}
	found := false
	for _, issue := range report.Critique.Issues {
		if issue.Message == "the hero says nothing the reader did not already know" {
			found = true
		}
	}
	if !found {
		t.Errorf("the critic's own finding was dropped: %+v", report.Critique.Issues)
	}

	var stored struct {
		Exists   bool                `json:"exists"`
		Critique design.Critique     `json:"critique"`
		Decision design.GateDecision `json:"decision"`
	}
	if code := call(t, s, http.MethodGet,
		"/api/v1/design/artifacts/"+artifact.ID+"/critique", "", &stored); code != http.StatusOK {
		t.Fatalf("GET critique after = %d", code)
	}
	if !stored.Exists {
		t.Fatal("the recorded pass is not readable back")
	}
	if stored.Critique.Score != report.Critique.Score {
		t.Errorf("stored score %.1f, reported %.1f", stored.Critique.Score, report.Critique.Score)
	}
	if stored.Decision.Reason == "" {
		t.Error("the decision came back without a reason a user can read")
	}
}

// A pass the caller asks not to record leaves no history: a panel previewing a
// stricter policy must not write it into the artifact's record.
func TestDesignCritiqueCanRunWithoutRecording(t *testing.T) {
	s, artifact := designStudioServer(t)

	var report design.CritiqueReport
	if code := call(t, s, http.MethodPost,
		"/api/v1/design/artifacts/"+artifact.ID+"/critique",
		`{"skip_render":true,"record":false,"policy":"strict"}`, &report); code != http.StatusOK {
		t.Fatalf("POST critique = %d", code)
	}
	if report.Recorded {
		t.Error("record:false still wrote a critique")
	}
	if report.Decision.Policy != design.PolicyStrict {
		t.Errorf("policy override ignored: %+v", report.Decision)
	}

	var stored struct {
		Exists bool `json:"exists"`
	}
	call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+artifact.ID+"/critique", "", &stored)
	if stored.Exists {
		t.Error("a dry pass ended up in the history")
	}
}

func TestDesignCritiqueRejectsAnUnknownArtifact(t *testing.T) {
	s, _ := designStudioServer(t)
	if code := call[struct{}](t, s, http.MethodPost,
		"/api/v1/design/artifacts/dsg_nope/critique", `{"skip_render":true}`, nil); code != http.StatusNotFound {
		t.Errorf("critique of a missing artifact = %d, want 404", code)
	}
}
