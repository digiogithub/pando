package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/spf13/viper"
)

func TestResolveSecretRef(t *testing.T) {
	t.Setenv("PANDO_TEST_REF_SET", "s3cret")
	t.Setenv("PANDO_TEST_REF_EMPTY", "")
	os.Unsetenv("PANDO_TEST_REF_UNSET")

	cases := []struct{ in, want string }{
		{"literal-key", "literal-key"},
		{"", ""},
		{"env:PANDO_TEST_REF_SET", "s3cret"},
		{"env:PANDO_TEST_REF_UNSET", ""},
		{"env:PANDO_TEST_REF_EMPTY", ""},
		{"env:1BAD", "env:1BAD"}, // not a valid variable name: literal
		{"env:A B", "env:A B"},   // not a reference
		{"xenv:PANDO_TEST_REF_SET", "xenv:PANDO_TEST_REF_SET"},
	}
	for _, c := range cases {
		if got := ResolveSecretRef(c.in); got != c.want {
			t.Errorf("ResolveSecretRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if !IsSecretRef("env:PANDO_TEST_REF_SET") || IsSecretRef("sk-abc") {
		t.Error("IsSecretRef misclassified a value")
	}
}

func TestProviderAccountResolvedAtUse(t *testing.T) {
	t.Setenv("PANDO_TEST_KEY", "first")
	t.Setenv("PANDO_TEST_HDR", "hdr-value")
	acc := ProviderAccount{
		ID: "x", Type: models.ProviderOpenAICompatible,
		APIKey:       "env:PANDO_TEST_KEY",
		ExtraHeaders: map[string]string{"X-Token": "env:PANDO_TEST_HDR", "X-Static": "plain"},
	}
	r := acc.Resolved()
	if r.APIKey != "first" || r.ExtraHeaders["X-Token"] != "hdr-value" || r.ExtraHeaders["X-Static"] != "plain" {
		t.Fatalf("unexpected resolution: %+v", r)
	}
	if acc.APIKey != "env:PANDO_TEST_KEY" || acc.ExtraHeaders["X-Token"] != "env:PANDO_TEST_HDR" {
		t.Fatalf("receiver was modified: %+v", acc)
	}
	// Resolution happens at use, so a changed environment is picked up.
	t.Setenv("PANDO_TEST_KEY", "second")
	if got := acc.Resolved().APIKey; got != "second" {
		t.Fatalf("expected re-resolution, got %q", got)
	}
}

// loadWithConfig loads a project config with the given JSON and an isolated home.
func loadWithConfig(t *testing.T, body string) (projectDir string) {
	t.Helper()
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})
	home := t.TempDir()
	projectDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".pando.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "OLLAMA_BASE_URL", "OLLAMA_API_KEY", "LLAMACPP_BASE_URL"} {
		t.Setenv(k, "")
	}
	return projectDir
}

func TestEnvProviderKeptBesideOpenAICompatibleAccount(t *testing.T) {
	body := `{"providerAccounts":[
	  {"id":"local-llm","displayName":"Local","type":"openai-compatible","baseUrl":"http://127.0.0.1:11434/v1","apiKey":"env:PANDO_TEST_LLM_KEY"},
	  {"id":"openai-main","displayName":"OpenAI","type":"openai","apiKey":"sk-file"}
	]}`
	dir := loadWithConfig(t, body)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env")
	t.Setenv("OPENAI_API_KEY", "sk-openai-env")
	t.Setenv("PANDO_TEST_LLM_KEY", "llm-secret")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byID := map[string]ProviderAccount{}
	for _, a := range loaded.ProviderAccounts {
		byID[a.ID] = a
	}
	ant, ok := byID["anthropic"]
	if !ok || ant.Type != models.ProviderAnthropic || ant.APIKey != "sk-ant-env" {
		t.Fatalf("env-only anthropic provider was lost: %+v", loaded.ProviderAccounts)
	}
	// openai already has an account: it must not be duplicated nor overridden.
	if _, dup := byID["openai"]; dup {
		t.Fatalf("openai type duplicated: %+v", loaded.ProviderAccounts)
	}
	if byID["openai-main"].APIKey != "sk-file" {
		t.Fatalf("explicit openai account altered: %+v", byID["openai-main"])
	}
	if got := loaded.ProviderAccounts[0].ID; got != "local-llm" {
		t.Fatalf("explicit accounts must stay first, got %q", got)
	}
	// Reference stays a reference in the stored account, resolves at use.
	if byID["local-llm"].APIKey != "env:PANDO_TEST_LLM_KEY" {
		t.Fatalf("reference was resolved in place: %q", byID["local-llm"].APIKey)
	}
	if byID["local-llm"].Disabled {
		t.Fatal("account with a resolvable reference must not be disabled")
	}
	acc, err := ResolveProviderAccountForType(models.ProviderOpenAICompatible)
	if err != nil || acc.APIKey != "llm-secret" || acc.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("ResolveProviderAccountForType = %+v, %v", acc, err)
	}
	acc, err = ResolveProviderAccountByID("local-llm")
	if err != nil || acc.APIKey != "llm-secret" {
		t.Fatalf("ResolveProviderAccountByID = %+v, %v", acc, err)
	}
	if acc, err = ResolveProviderAccountForType(models.ProviderAnthropic); err != nil || acc.APIKey != "sk-ant-env" {
		t.Fatalf("env provider not resolvable by type: %+v, %v", acc, err)
	}
	if loaded.Providers[models.ProviderOpenAICompatible].APIKey != "llm-secret" {
		t.Fatalf("legacy providers map not synced with resolved key")
	}
}

func TestUnsetEnvReferenceBehavesKeyless(t *testing.T) {
	dir := loadWithConfig(t, `{"providerAccounts":[{"id":"local-llm","displayName":"Local","type":"openai-compatible","baseUrl":"http://x/v1","apiKey":"env:PANDO_TEST_NEVER_SET"}]}`)
	os.Unsetenv("PANDO_TEST_NEVER_SET")
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	acc, err := ResolveProviderAccountByID("local-llm")
	if err != nil || acc.APIKey != "" {
		t.Fatalf("expected empty key, got %+v, %v", acc, err)
	}
}

func TestSavingAccountsNeverPersistsResolvedSecret(t *testing.T) {
	dir := loadWithConfig(t, `{"providerAccounts":[{"id":"local-llm","displayName":"Local","type":"openai-compatible","baseUrl":"http://x/v1","apiKey":"env:PANDO_TEST_SAVE_KEY","extraHeaders":{"X-Token":"env:PANDO_TEST_SAVE_HDR"}}]}`)
	t.Setenv("PANDO_TEST_SAVE_KEY", "TOPSECRET-KEY")
	t.Setenv("PANDO_TEST_SAVE_HDR", "TOPSECRET-HDR")
	t.Setenv("ANTHROPIC_API_KEY", "TOPSECRET-ENV-PROVIDER")
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := AddProviderAccount(ProviderAccount{ID: "second", DisplayName: "Second", Type: models.ProviderOpenAICompatible, BaseURL: "http://y/v1", APIKey: "env:PANDO_TEST_SAVE_KEY"}); err != nil {
		t.Fatalf("AddProviderAccount: %v", err)
	}
	if err := SetProviderAccountDisabled("second", false); err != nil {
		t.Fatalf("SetProviderAccountDisabled: %v", err)
	}
	if err := UpdateProviderAccount("local-llm", ProviderAccount{DisplayName: "Local 2", Type: models.ProviderOpenAICompatible, BaseURL: "http://x/v1", APIKey: "env:PANDO_TEST_SAVE_KEY", ExtraHeaders: map[string]string{"X-Token": "env:PANDO_TEST_SAVE_HDR"}}); err != nil {
		t.Fatalf("UpdateProviderAccount: %v", err)
	}
	var found bool
	for _, root := range []string{dir, os.Getenv("HOME")} {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.Contains(filepath.Base(p), "pando") {
				return nil
			}
			data, _ := os.ReadFile(p)
			s := string(data)
			if strings.Contains(s, "TOPSECRET") {
				t.Errorf("%s contains a resolved secret:\n%s", p, s)
			}
			if strings.Contains(s, "env:PANDO_TEST_SAVE_KEY") {
				found = true
			}
			if regexp.MustCompile(`"id":\s*"anthropic"`).MatchString(s) {
				t.Errorf("%s persisted the merged environment provider:\n%s", p, s)
			}
			return nil
		})
	}
	if !found {
		t.Error("the env reference was not found in any saved config file")
	}
}
