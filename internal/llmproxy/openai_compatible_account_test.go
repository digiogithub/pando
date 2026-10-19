package llmproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/config"
)

// fakeUpstream is an OpenAI-compatible server that records what it receives.
type fakeUpstream struct {
	srv *httptest.Server
	mu  sync.Mutex
	// hits maps "METHOD path" to the recorded request bodies.
	hits map[string][]string
	// auth and headers map "METHOD path" to the last request headers seen.
	headers map[string]http.Header
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{hits: map[string][]string{}, headers: map[string]http.Header{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.hits[key] = append(f.hits[key], string(body))
		f.headers[key] = r.Header.Clone()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch key {
		case "GET /v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"fake-chat","object":"model"},{"id":"fake-embed","object":"model"}]}`)
		case "POST /v1/chat/completions":
			fmt.Fprint(w, `{"id":"c1","object":"chat.completion","created":1,"model":"fake-chat","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		case "POST /v1/embeddings":
			fmt.Fprint(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"fake-embed"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) bodies(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits[key]...)
}

func (f *fakeUpstream) header(key string) http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers[key]
}

// loadProxyConfig loads a project config declaring one openai-compatible
// account (key and header by env reference) whose baseUrl is the fake server.
func loadProxyConfig(t *testing.T, baseURL string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	home, project := t.TempDir(), t.TempDir()
	body := fmt.Sprintf(`{"providerAccounts":[{"id":"local-llm","displayName":"Local","type":"openai-compatible","baseUrl":%q,"apiKey":"env:PANDO_TEST_PROXY_KEY","extraHeaders":{"X-Tenant":"env:PANDO_TEST_PROXY_TENANT"}}]}`, baseURL+"/v1")
	if err := os.WriteFile(filepath.Join(project, ".pando.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PANDO_TEST_PROXY_KEY", "proxy-secret")
	t.Setenv("PANDO_TEST_PROXY_TENANT", "tenant-7")
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "OLLAMA_BASE_URL", "OLLAMA_API_KEY", "LLAMACPP_BASE_URL"} {
		t.Setenv(k, "")
	}
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
}

func proxyDo(t *testing.T, s *LLMProxyServer, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func TestProxyServesOpenAICompatibleAccount(t *testing.T) {
	up := newFakeUpstream(t)
	loadProxyConfig(t, up.srv.URL)
	s := NewServer(ProxyConfig{})

	// The background refresh loop populates the registry at startup; run the
	// same per-account refresh the proxy uses.
	refreshAccountModels(context.Background(), activeAccounts())

	// /v1/models lists the account's models, fetched from {baseUrl}/models.
	rec := proxyDo(t, s, http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("models status %d: %s", rec.Code, rec.Body.String())
	}
	var list openAIModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var chatID, embedID string
	for _, m := range list.Data {
		if strings.HasSuffix(m.ID, "fake-chat") {
			chatID = m.ID
		}
		if strings.HasSuffix(m.ID, "fake-embed") {
			embedID = m.ID
		}
	}
	if chatID == "" || embedID == "" {
		t.Fatalf("account models missing from listing: %+v", list.Data)
	}
	if got := up.header("GET /v1/models").Get("Authorization"); got != "Bearer proxy-secret" {
		t.Errorf("model listing Authorization = %q, want the env-resolved key", got)
	}

	// Chat completions reach {baseUrl}/chat/completions with the bare model.
	for _, requested := range []string{"fake-chat", chatID} {
		before := len(up.bodies("POST /v1/chat/completions"))
		rec = proxyDo(t, s, http.MethodPost, "/v1/chat/completions",
			fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}]}`, requested))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "pong") {
			t.Fatalf("chat(%s) status %d: %s", requested, rec.Code, rec.Body.String())
		}
		bodies := up.bodies("POST /v1/chat/completions")
		if len(bodies) != before+1 {
			t.Fatalf("chat(%s): upstream not called", requested)
		}
		var sent struct{ Model string }
		_ = json.Unmarshal([]byte(bodies[len(bodies)-1]), &sent)
		if sent.Model != "fake-chat" {
			t.Errorf("chat(%s): upstream model = %q, want fake-chat", requested, sent.Model)
		}
	}
	h := up.header("POST /v1/chat/completions")
	if h.Get("Authorization") != "Bearer proxy-secret" || h.Get("X-Tenant") != "tenant-7" {
		t.Errorf("chat upstream headers not resolved from env: %v", h)
	}

	// Embeddings go to {baseUrl}/embeddings and the model arrives bare, with
	// every other field of the request preserved.
	for _, requested := range []string{"fake-embed", embedID, "openai-compatible.fake-embed", "local-llm.fake-embed"} {
		before := len(up.bodies("POST /v1/embeddings"))
		rec = proxyDo(t, s, http.MethodPost, "/v1/embeddings",
			fmt.Sprintf(`{"model":%q,"input":"hello","dimensions":2}`, requested))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "embedding") {
			t.Fatalf("embeddings(%s) status %d: %s", requested, rec.Code, rec.Body.String())
		}
		bodies := up.bodies("POST /v1/embeddings")
		if len(bodies) != before+1 {
			t.Fatalf("embeddings(%s): upstream not called", requested)
		}
		var sent struct {
			Model      string
			Input      string
			Dimensions int
		}
		_ = json.Unmarshal([]byte(bodies[len(bodies)-1]), &sent)
		if sent.Model != "fake-embed" || sent.Input != "hello" || sent.Dimensions != 2 {
			t.Errorf("embeddings(%s): upstream body = %s", requested, bodies[len(bodies)-1])
		}
	}
	h = up.header("POST /v1/embeddings")
	if h.Get("Authorization") != "Bearer proxy-secret" || h.Get("X-Tenant") != "tenant-7" {
		t.Errorf("embeddings upstream headers not resolved from env: %v", h)
	}
}
