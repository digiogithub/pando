package preview

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The canvas is a capability like every other preview route: the token in the
// path is the only way in, and a wrong one must not confirm that a canvas
// exists at all.
func TestCanvasNeedsItsToken(t *testing.T) {
	server := New(Options{})
	grant, err := server.PublishCanvas("session-1")
	if err != nil {
		t.Fatalf("PublishCanvas: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{"the page", CanvasPath + grant.Token + "/", http.StatusOK},
		{"its script", CanvasPath + grant.Token + "/canvas.js", http.StatusOK},
		{"its state", CanvasPath + grant.Token + "/artboards", http.StatusOK},
		{"a wrong token", CanvasPath + "deadbeef/", http.StatusNotFound},
		{"a path below it", CanvasPath + grant.Token + "/../../etc/passwd", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != tc.want {
				t.Fatalf("GET %s = %d, want %d", tc.path, recorder.Code, tc.want)
			}
		})
	}
}

// One canvas per session: presenting it twice must return the window already
// open, not a second one looking at the same artboards.
func TestCanvasIsStablePerSession(t *testing.T) {
	server := New(Options{})
	first, err := server.PublishCanvas("session-1")
	if err != nil {
		t.Fatalf("PublishCanvas: %v", err)
	}
	second, err := server.PublishCanvas("session-1")
	if err != nil {
		t.Fatalf("PublishCanvas again: %v", err)
	}
	if first.Token != second.Token {
		t.Fatalf("token changed between publishes: %s then %s", first.Token, second.Token)
	}

	other, err := server.PublishCanvas("session-2")
	if err != nil {
		t.Fatalf("PublishCanvas other session: %v", err)
	}
	if other.Token == first.Token {
		t.Fatal("two sessions share one canvas token")
	}

	url, err := server.CanvasURL("session-1")
	if err != nil {
		t.Fatalf("CanvasURL: %v", err)
	}
	if !strings.Contains(url, first.Token) {
		t.Fatalf("CanvasURL %q does not carry the session token", url)
	}
	if _, err := server.CanvasURL("session-none"); err == nil {
		t.Fatal("an unpublished session returned a canvas URL")
	}
}

// The artboards route reports what the provider returns, and reports a provider
// failure as an error field rather than as a broken page: a canvas that goes
// blank the moment one artifact misbehaves is worse than one that says so.
func TestCanvasArtboardsPayload(t *testing.T) {
	server := New(Options{
		Artboards: func(sessionID string) ([]Artboard, error) {
			if sessionID != "session-1" {
				return nil, errors.New("wrong session")
			}
			return []Artboard{
				{ID: "b", Title: "Second", URL: "/preview/t2/index.html", Width: 1280, Height: 720},
				{ID: "a", Title: "First", URL: "/preview/t1/index.html", Width: 1440, Height: 900},
			}, nil
		},
	})
	grant, err := server.PublishCanvas("session-1")
	if err != nil {
		t.Fatalf("PublishCanvas: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, CanvasPath+grant.Token+"/artboards", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("artboards = %d", recorder.Code)
	}
	var state canvasState
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.Error != "" {
		t.Fatalf("unexpected error field: %s", state.Error)
	}
	if len(state.Artboards) != 2 {
		t.Fatalf("got %d artboards, want 2", len(state.Artboards))
	}
	// Sorted by id, so an artboard keeps its place on the canvas between polls.
	if state.Artboards[0].ID != "a" || state.Artboards[1].ID != "b" {
		t.Fatalf("artboards are not in a stable order: %v", state.Artboards)
	}
}

func TestCanvasReportsProviderFailure(t *testing.T) {
	server := New(Options{
		Artboards: func(string) ([]Artboard, error) { return nil, errors.New("no design provider") },
	})
	grant, err := server.PublishCanvas("")
	if err != nil {
		t.Fatalf("PublishCanvas: %v", err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, CanvasPath+grant.Token+"/artboards", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("artboards = %d, want a served page carrying the error", recorder.Code)
	}
	var state canvasState
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(state.Error, "no design provider") {
		t.Fatalf("error not reported to the page: %+v", state)
	}
	if state.Artboards == nil {
		t.Fatal("artboards must be an empty list, never null")
	}
}

// A session that ends takes its canvas with it: a window left open must stop
// resolving rather than keep serving whatever the directory now holds.
func TestRevokeSessionClosesTheCanvas(t *testing.T) {
	server := New(Options{})
	grant, err := server.PublishCanvas("session-1")
	if err != nil {
		t.Fatalf("PublishCanvas: %v", err)
	}
	server.RevokeSession("session-1")

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, CanvasPath+grant.Token+"/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("a revoked canvas still serves: %d", recorder.Code)
	}
}

// The access guard covers the canvas as well as the artifacts: publishing one
// on a network-facing listener with no authentication must be refused.
func TestCanvasHonoursTheAccessGuard(t *testing.T) {
	refused := errors.New("preview: external access without basic auth")
	server := New(Options{Access: func() error { return refused }})
	if _, err := server.PublishCanvas("session-1"); !errors.Is(err, refused) {
		t.Fatalf("PublishCanvas error = %v, want %v", err, refused)
	}
}
