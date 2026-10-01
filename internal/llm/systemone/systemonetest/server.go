// Package systemonetest provides a scriptable fake System One server that
// emulates Ollama 0.35 and Jev-compatible remote gateways. It is used by Go
// tests and by the WebUI Playwright run.
package systemonetest

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mode selects which backend is emulated.
type Mode int

const (
	ModeOllama Mode = iota
	ModeRemote
)

// Recorded is one request seen by the server.
type Recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// Server is the fake backend.
type Server struct {
	*httptest.Server

	mode       Mode
	version    string
	tagsJSON   string
	showJSON   string
	apiKey     string
	modelsJSON string
	modelsCode int // when non-zero, /v1/models answers with this status
	models     map[string]bool

	mu       sync.Mutex
	reqs     []Recorded
	failNext []int
	delay    time.Duration
	choice   string
	probs    map[string]float64
	perQ     map[string]scripted
	cost     *float64
	rawResp  string
}

// Option customises a Server.
type Option func(*Server)

// WithMode sets the emulated backend.
func WithMode(m Mode) Option { return func(s *Server) { s.mode = m } }

// WithVersion overrides the /api/version value (Ollama mode).
func WithVersion(v string) Option { return func(s *Server) { s.version = v } }

// WithTagsJSON overrides the /api/tags body (Ollama mode).
func WithTagsJSON(j string) Option { return func(s *Server) { s.tagsJSON = j } }

// WithShowJSON overrides the /api/show body (Ollama mode).
func WithShowJSON(j string) Option { return func(s *Server) { s.showJSON = j } }

// WithAPIKey requires "Authorization: Bearer <key>" on every request.
func WithAPIKey(k string) Option { return func(s *Server) { s.apiKey = k } }

// WithModelsJSON sets the /v1/models body (remote mode) and the set of ids
// accepted by /v1/systemone.
func WithModelsJSON(j string) Option { return func(s *Server) { s.modelsJSON = j } }

// WithModelsStatus makes /v1/models answer with the given status (e.g. 404).
func WithModelsStatus(code int) Option { return func(s *Server) { s.modelsCode = code } }

// WithAcceptedModels restricts the model ids accepted by /v1/systemone.
func WithAcceptedModels(ids ...string) Option {
	return func(s *Server) {
		s.models = map[string]bool{}
		for _, id := range ids {
			s.models[id] = true
		}
	}
}

// New starts a fake server (Ollama 0.35 by default) closed by t.Cleanup.
func New(t testing.TB, opts ...Option) *Server {
	s := &Server{
		mode:     ModeOllama,
		version:  "0.35.0",
		tagsJSON: OllamaTagsJSON,
		showJSON: OllamaShowTev1JSON,
	}
	for _, o := range opts {
		o(s)
	}
	if s.mode == ModeOllama && s.models == nil {
		s.models = map[string]bool{"tev1:0.8b": true}
	}
	if s.mode == ModeRemote && s.modelsJSON == "" {
		s.modelsJSON = TypeSafeModelsJSON
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Server.Close)
	return s
}

// NewOllama035 starts a server serving the recorded Ollama 0.35.0 fixtures.
func NewOllama035(t testing.TB, opts ...Option) *Server { return New(t, opts...) }

// NewRemote starts a remote Jev-compatible gateway emulation.
func NewRemote(t testing.TB, opts ...Option) *Server {
	return New(t, append([]Option{WithMode(ModeRemote)}, opts...)...)
}

// BaseURL is the server root (same as the embedded URL).
func (s *Server) BaseURL() string { return s.Server.URL }

// SetDecision scripts the answer returned for every choice question. A nil
// probs map gives probability 1 to choice.
func (s *Server) SetDecision(choice string, probs map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.choice, s.probs, s.rawResp = choice, probs, ""
}

type scripted struct {
	choice string
	probs  map[string]float64
}

// SetQuestionDecision scripts the answer for one named question, overriding
// SetDecision for that question only.
func (s *Server) SetQuestionDecision(question, choice string, probs map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perQ == nil {
		s.perQ = map[string]scripted{}
	}
	s.perQ[question] = scripted{choice, probs}
}

// SetRawResponse scripts a verbatim 200 body for /v1/systemone.
func (s *Server) SetRawResponse(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawResp = body
}

// SetCost makes responses carry usage.cost.
func (s *Server) SetCost(c float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cost = &c
}

// FailNext queues an error status for the next request (any path). Several
// calls queue several failures, consumed in order.
func (s *Server) FailNext(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = append(s.failNext, status)
}

// Delay makes every request sleep d before answering.
func (s *Server) Delay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// Requests returns a copy of the request log.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recorded(nil), s.reqs...)
}

// Count returns how many requests hit path.
func (s *Server) Count(path string) int {
	n := 0
	for _, r := range s.Requests() {
		if r.Path == path {
			n++
		}
	}
	return n
}

// Reset clears the request log and scripted failures.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs, s.failNext = nil, nil
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	b, _ := json.Marshal(map[string]string{"error": msg})
	writeJSON(w, code, string(b))
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	s.mu.Lock()
	s.reqs = append(s.reqs, Recorded{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	delay := s.delay
	var fail int
	if len(s.failNext) > 0 {
		fail, s.failNext = s.failNext[0], s.failNext[1:]
	}
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if fail != 0 {
		writeErr(w, fail, "scripted failure")
		return
	}
	if s.apiKey != "" && r.Header.Get("Authorization") != "Bearer "+s.apiKey {
		writeErr(w, http.StatusUnauthorized, "invalid api key")
		return
	}

	switch r.URL.Path {
	case "/api/version":
		if s.mode != ModeOllama {
			http.NotFound(w, r)
			return
		}
		b, _ := json.Marshal(map[string]string{"version": s.version})
		writeJSON(w, 200, string(b))
	case "/api/tags":
		if s.mode != ModeOllama {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, s.tagsJSON)
	case "/api/pull":
		// Additive: a scripted pull that streams progress then success. It
		// never downloads anything.
		if s.mode != ModeOllama || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, "{\"status\":\"pulling manifest\"}\n")
		_, _ = io.WriteString(w, "{\"status\":\"pulling abc\",\"total\":100,\"completed\":50}\n")
		_, _ = io.WriteString(w, "{\"status\":\"success\"}\n")
	case "/api/show":
		if s.mode != ModeOllama {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, s.showJSON)
	case "/v1/models":
		if s.mode != ModeRemote {
			http.NotFound(w, r)
			return
		}
		if s.modelsCode != 0 {
			writeErr(w, s.modelsCode, "no models endpoint")
			return
		}
		writeJSON(w, 200, s.modelsJSON)
	case "/v1/systemone":
		s.systemOne(w, body)
	default:
		http.NotFound(w, r)
	}
}

type q struct {
	Type     string          `json:"type"`
	Criteria json.RawMessage `json:"criteria"`
}

// criteriaKeys returns the criteria keys in document order.
func criteriaKeys(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			break
		}
	}
	return keys
}

func (s *Server) systemOne(w http.ResponseWriter, body []byte) {
	if len(body) > 64*1024 {
		writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	var req struct {
		Model     string       `json:"model"`
		Questions map[string]q `json:"questions"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.HasSuffix(req.Model, ":cloud") {
		writeErr(w, 400, "cloud models are not supported")
		return
	}
	if s.models != nil && !s.models[req.Model] {
		writeErr(w, 404, "model \""+req.Model+"\" not found, try pulling it first")
		return
	}
	if n := len(req.Questions); n < 1 || n > 64 {
		writeErr(w, 400, "questions must contain 1-64 entries")
		return
	}

	s.mu.Lock()
	raw, choice, probs, cost := s.rawResp, s.choice, s.probs, s.cost
	perQ := make(map[string]scripted, len(s.perQ))
	for k, v := range s.perQ {
		perQ[k] = v
	}
	s.mu.Unlock()
	if raw != "" {
		writeJSON(w, 200, raw)
		return
	}

	names := make([]string, 0, len(req.Questions))
	for n := range req.Questions {
		names = append(names, n)
	}
	sort.Strings(names)
	answers := map[string]any{}
	for _, n := range names {
		qq := req.Questions[n]
		keys := criteriaKeys(qq.Criteria)
		if qq.Type == "choice" && (len(keys) < 2 || len(keys) > 26) {
			writeErr(w, 400, "question \""+n+"\": criteria must contain 2–26 candidates")
			return
		}
		c, p := choice, probs
		if sc, ok := perQ[n]; ok {
			c, p = sc.choice, sc.probs
		}
		if c == "" && len(keys) > 0 {
			c = keys[0]
		}
		if p == nil {
			p = map[string]float64{c: 1}
		}
		answers[n] = map[string]any{"type": qq.Type, "choice": c, "probabilities": p, "confidence": confidence(p)}
	}
	usage := map[string]any{"input_tokens": 111, "output_tokens": 1}
	if cost != nil {
		usage["cost"] = *cost
	}
	out, _ := json.Marshal(map[string]any{"model": req.Model, "answers": answers, "usage": usage})
	writeJSON(w, 200, string(out))
}

// confidence = 1 - H(p)/ln(N), the entropy concentration used by System One.
func confidence(p map[string]float64) float64 {
	n := len(p)
	if n < 2 {
		return 1
	}
	var h float64
	for _, v := range p {
		if v > 0 {
			h -= v * math.Log(v)
		}
	}
	return 1 - h/math.Log(float64(n))
}
