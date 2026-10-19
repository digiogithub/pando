package llmproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/config"
)

// TestListModelsFetchesKeylessAccountBesideStaticProvider guards the live
// finding: a keyless OpenAI-compatible account (a private Ollama gateway) must
// be listed by /v1/models even though a static provider (Gemini, from the
// environment) already populates the registry, and an unreachable account must
// not break the listing.
func TestListModelsFetchesKeylessAccountBesideStaticProvider(t *testing.T) {
	var gotAuth string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"nomic-embed-text:v1.5","object":"model"},{"id":"tev1:0.8b","object":"model"}]}`)
	}))
	t.Cleanup(fake.Close)

	config.IsolateForTests(t)
	viper.Reset()
	t.Cleanup(viper.Reset)
	home, project := t.TempDir(), t.TempDir()
	body := fmt.Sprintf(`{"providerAccounts":[`+
		`{"id":"ollama-local","type":"openai-compatible","baseUrl":%q},`+
		`{"id":"down","type":"openai-compatible","baseUrl":"http://127.0.0.1:1/v1"}]}`, fake.URL+"/v1")
	if err := os.WriteFile(filepath.Join(project, ".pando.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "OLLAMA_BASE_URL", "OLLAMA_API_KEY", "LLAMACPP_BASE_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv("GEMINI_API_KEY", "dummy")
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	s := NewServer(ProxyConfig{})

	rec := proxyDo(t, s, http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var list openAIModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var gemini, embed, tev bool
	for _, m := range list.Data {
		switch {
		case strings.HasPrefix(m.ID, "gemini."):
			gemini = true
		case m.ID == "ollama-local.nomic-embed-text:v1.5":
			embed = true
		case m.ID == "ollama-local.tev1:0.8b":
			tev = true
		}
	}
	if !gemini || !embed || !tev {
		t.Fatalf("listing incomplete (gemini=%v embed=%v tev=%v): %+v", gemini, embed, tev, list.Data)
	}
	if gotAuth != "" {
		t.Errorf("keyless account sent Authorization %q", gotAuth)
	}
}
