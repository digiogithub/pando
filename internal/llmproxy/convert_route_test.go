package llmproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/convert"
)

func convertRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/convert", strings.NewReader("name,value\nPando,42\n"))
	req.Header.Set("Content-Type", "text/csv")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestConvertRouteIsBehindTheAPIKey(t *testing.T) {
	h := NewServer(ProxyConfig{
		APIKey:  "k",
		Convert: convert.NewHTTPHandler(convert.HTTPOptions{Runner: convert.InProcessRunner{Converter: convert.New()}}),
	}).httpServer.Handler

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, convertRequest(""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token: status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, convertRequest("wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, convertRequest("k"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Pando") {
		t.Fatalf("with token: status %d: %s", rec.Code, rec.Body)
	}
}

func TestConvertRouteIsOffByDefault(t *testing.T) {
	h := NewServer(ProxyConfig{APIKey: "k"}).httpServer.Handler
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, convertRequest("k"))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want the route to be absent", rec.Code)
	}
}
