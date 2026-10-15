package llmproxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func env(v string) func(string) string {
	return func(k string) string {
		if k == APIKeyEnv {
			return v
		}
		return ""
	}
}

func TestResolveAPIKeyPrecedence(t *testing.T) {
	file := writeKeyFile(t, "from-file\n")
	cases := []struct {
		name, flag, file, env, want string
	}{
		{"flag only", "from-flag", "", "from-env", "from-flag"},
		{"file beats env", "", file, "from-env", "from-file"},
		{"env only", "", "", "from-env", "from-env"},
		{"none disables auth", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveAPIKey(c.flag, c.file, env(c.env))
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestResolveAPIKeyFlagAndFileConflict(t *testing.T) {
	if _, err := ResolveAPIKey("k", writeKeyFile(t, "x"), env("")); err == nil {
		t.Fatal("expected an error when both flag and file are set")
	}
}

func TestResolveAPIKeyFileContents(t *testing.T) {
	for in, want := range map[string]string{"k": "k", "k\n": "k", "k\r\n": "k", "k\n\n": "k\n", " k ": " k "} {
		got, err := ResolveAPIKey("", writeKeyFile(t, in), env(""))
		if err != nil || got != want {
			t.Errorf("file %q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestResolveAPIKeyFileErrors(t *testing.T) {
	for _, in := range []string{"", "\n", "\r\n"} {
		if _, err := ResolveAPIKey("", writeKeyFile(t, in), env("fallback")); err == nil {
			t.Errorf("file %q: expected empty-file error (no env fallback)", in)
		}
	}
	if _, err := ResolveAPIKey("", filepath.Join(t.TempDir(), "missing"), env("")); err == nil {
		t.Error("expected an error for an unreadable file")
	}
}

func TestKeyMatches(t *testing.T) {
	if !keyMatches("secret", "secret") {
		t.Error("equal keys must match")
	}
	for _, bad := range []string{"", "secre", "secrets", "SECRET", strings.Repeat("s", 4096)} {
		if keyMatches("secret", bad) {
			t.Errorf("%q must not match", bad)
		}
	}
}

func TestAuthMiddlewareConstantTimePath(t *testing.T) {
	h := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	do := func(hdr, val string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		if hdr != "" {
			req.Header.Set(hdr, val)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := do("Authorization", "Bearer secret"); c != http.StatusNoContent {
		t.Errorf("bearer ok: %d", c)
	}
	if c := do("x-api-key", "secret"); c != http.StatusNoContent {
		t.Errorf("x-api-key ok: %d", c)
	}
	if c := do("Authorization", "Bearer wrong"); c != http.StatusUnauthorized {
		t.Errorf("bearer wrong: %d", c)
	}
	if c := do("x-api-key", "secretX"); c != http.StatusUnauthorized {
		t.Errorf("x-api-key wrong: %d", c)
	}
	if c := do("", ""); c != http.StatusUnauthorized {
		t.Errorf("missing: %d", c)
	}
}
