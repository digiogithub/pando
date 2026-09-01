package preview

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The canvas is the read-only counterpart of the single-artifact preview: one
// pan-and-zoom surface holding every artifact of a session as an artboard, so a
// user watching an agent design something sees the work appear and change
// without having to open a tab per artifact.
//
// It is deliberately view-only. The artboards are live documents in iframes, but
// a transparent capture layer sits over each one: the user pans, zooms and
// reads, and never edits by hand. Editing stays the agent's job, which is what
// keeps the file on disk the single source of truth.
//
// Like an artifact grant, a canvas grant is a capability: the token in the path
// is the only way in, it is bound to a session, and it expires.

// CanvasSegment is the token-free first segment of every canvas URL. It cannot
// collide with an artifact token because tokens are hex and this is not.
const CanvasSegment = "_canvas"

// CanvasPath is the route prefix the canvas owns.
const CanvasPath = Prefix + CanvasSegment + "/"

// CanvasGrant is one canvas published for viewing.
type CanvasGrant struct {
	Token     string    `json:"token"`
	SessionID string    `json:"session_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Artboard is one artifact as the canvas sees it. The preview package knows
// nothing about the design model, so the whole struct is filled in by the
// [Options.Artboards] provider that internal/design installs.
type Artboard struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug,omitempty"`
	Kind  string `json:"kind,omitempty"`
	// URL is the address of the artifact document the artboard frames.
	URL string `json:"url"`
	// Width and Height are the logical viewport the artboard is rendered at,
	// before the canvas zoom scales it. A deck is 1280x720, a page 1440x900.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Version is the artifact's snapshot number, shown on the artboard label.
	Version int `json:"version,omitempty"`
	// Status is "ready", "building" or "error". The canvas renders a badge for
	// anything that is not ready.
	Status string `json:"status,omitempty"`
	// Note carries the error message, or whatever the status needs to explain.
	Note string `json:"note,omitempty"`
	// Revision changes whenever the document underneath changes. The canvas
	// watches it to flash the artboard and to force a reload of its frame.
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// canvasState is the payload the page polls.
type canvasState struct {
	Artboards []Artboard `json:"artboards"`
	// ServedAt lets the page show when it last heard from the server, which is
	// the difference between "nothing is happening" and "the poll is broken".
	ServedAt time.Time `json:"served_at"`
	Error    string    `json:"error,omitempty"`
}

//go:embed canvas.html
var canvasPage []byte

//go:embed canvas.js
var canvasScript []byte

// canvasRegistry is the canvas half of the grant table. It is a separate map
// from the artifact grants because a canvas has no directory and serves no
// files: sharing one map would mean every file route had to ask which kind of
// grant it just resolved.
type canvasRegistry struct {
	mu     sync.RWMutex
	grants map[string]*CanvasGrant // token -> grant
	// bySession keeps one canvas per session, so re-presenting the canvas
	// returns the URL already open in the user's browser instead of a second
	// window looking at the same thing.
	bySession map[string]string
}

func newCanvasRegistry() *canvasRegistry {
	return &canvasRegistry{
		grants:    make(map[string]*CanvasGrant),
		bySession: make(map[string]string),
	}
}

// PublishCanvas mints (or refreshes) the canvas grant of a session and returns
// it. Sessions without an id share the process-wide canvas, which is what a
// plain CLI invocation wants.
func (s *Server) PublishCanvas(sessionID string) (CanvasGrant, error) {
	if err := s.checkAccess(); err != nil {
		return CanvasGrant{}, err
	}
	reg := s.canvas
	reg.mu.Lock()
	defer reg.mu.Unlock()

	token, ok := reg.bySession[sessionID]
	if !ok {
		var err error
		if token, err = newToken(); err != nil {
			return CanvasGrant{}, err
		}
		reg.bySession[sessionID] = token
	}
	grant := &CanvasGrant{
		Token:     token,
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(s.opts.TTL),
	}
	reg.grants[token] = grant
	reg.sweepLocked()
	return *grant, nil
}

// CanvasURL returns the address of a published canvas.
func (s *Server) CanvasURL(sessionID string) (string, error) {
	reg := s.canvas
	reg.mu.RLock()
	token, ok := reg.bySession[sessionID]
	grant := reg.grants[token]
	reg.mu.RUnlock()
	if !ok || grant == nil {
		return "", errors.New("preview: no canvas is published for this session")
	}
	u := &url.URL{Path: CanvasPath + token + "/"}
	return s.base() + u.String(), nil
}

// RevokeCanvas drops the canvas grant of a session.
func (s *Server) RevokeCanvas(sessionID string) {
	reg := s.canvas
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if token, ok := reg.bySession[sessionID]; ok {
		delete(reg.grants, token)
		delete(reg.bySession, sessionID)
	}
}

func (r *canvasRegistry) sweepLocked() {
	now := time.Now()
	for token, grant := range r.grants {
		if now.After(grant.ExpiresAt) {
			delete(r.grants, token)
			delete(r.bySession, grant.SessionID)
		}
	}
}

func (r *canvasRegistry) resolve(token string) (*CanvasGrant, bool) {
	r.mu.RLock()
	grant, ok := r.grants[token]
	r.mu.RUnlock()
	if !ok || time.Now().After(grant.ExpiresAt) {
		return nil, false
	}
	return grant, true
}

// serveCanvas handles everything under CanvasPath. The routes are:
//
//	/preview/_canvas/{token}/            the page
//	/preview/_canvas/{token}/canvas.js   its script
//	/preview/_canvas/{token}/artboards   the JSON the page polls
//
// The script sits inside the token space rather than beside the bridge because
// it is only ever loaded by a page that already holds a token, and keeping it
// there means the canvas adds no token-free route to the server.
func (s *Server) serveCanvas(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, CanvasPath)
	token, rest, _ := strings.Cut(rest, "/")
	grant, ok := s.canvas.resolve(token)
	if !ok {
		// Same silence an unknown artifact token gets: a wrong token must not
		// tell the holder that some other canvas exists.
		http.NotFound(w, r)
		return
	}

	s.writeSecurityHeaders(w)
	w.Header().Set("Cache-Control", "no-store, must-revalidate")

	switch rest {
	case "", "index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "canvas.html", time.Time{}, bytes.NewReader(canvasPage))
	case "canvas.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		http.ServeContent(w, r, "canvas.js", time.Time{}, bytes.NewReader(canvasScript))
	case "artboards":
		s.serveArtboards(w, grant)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveArtboards(w http.ResponseWriter, grant *CanvasGrant) {
	state := canvasState{ServedAt: time.Now().UTC()}
	if s.opts.Artboards == nil {
		state.Error = "this build serves no artboards"
	} else if boards, err := s.opts.Artboards(grant.SessionID); err != nil {
		state.Error = err.Error()
	} else {
		state.Artboards = boards
	}
	// A stable order keeps an artboard from jumping across the canvas between
	// two polls, which would make the surface unusable while an agent works.
	sort.SliceStable(state.Artboards, func(i, j int) bool {
		return state.Artboards[i].ID < state.Artboards[j].ID
	})
	if state.Artboards == nil {
		state.Artboards = []Artboard{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		// The response is already committed; there is nothing to say to the
		// client, and the page will simply keep the artboards it has.
		_ = fmt.Errorf("preview: encode canvas state: %w", err)
	}
}
