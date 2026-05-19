package antigravity

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCodeChallengeMatchesPKCES256(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if got := CodeChallenge(verifier); got != want {
		t.Fatalf("CodeChallenge() = %q, want %q", got, want)
	}
}

func TestSessionAuthURLIncludesPKCEAndOfflineAccess(t *testing.T) {
	session := Session{
		State:        "state-123",
		CodeVerifier: "verifier-123",
		RedirectURI:  "http://localhost/callback",
	}

	rawURL, err := session.AuthURL()
	if err != nil {
		t.Fatalf("AuthURL(): %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(): %v", err)
	}
	values := parsed.Query()

	if values.Get("state") != session.State {
		t.Fatalf("state = %q, want %q", values.Get("state"), session.State)
	}
	if values.Get("code_challenge") != CodeChallenge(session.CodeVerifier) {
		t.Fatalf("code_challenge = %q, want %q", values.Get("code_challenge"), CodeChallenge(session.CodeVerifier))
	}
	if values.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", values.Get("code_challenge_method"))
	}
	if values.Get("access_type") != "offline" {
		t.Fatalf("access_type = %q, want offline", values.Get("access_type"))
	}
	if values.Get("prompt") != "consent" {
		t.Fatalf("prompt = %q, want consent", values.Get("prompt"))
	}
}

func TestValidateCallbackRejectsStateMismatch(t *testing.T) {
	session := Session{State: "expected"}
	if err := session.ValidateCallback("other"); err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestNewSessionTrimsRedirectURIAndGeneratesFields(t *testing.T) {
	session, err := NewSession("  http://localhost/callback  ")
	if err != nil {
		t.Fatalf("NewSession(): %v", err)
	}
	if session.State == "" {
		t.Fatal("expected generated state")
	}
	if session.CodeVerifier == "" {
		t.Fatal("expected generated code verifier")
	}
	if session.RedirectURI != "http://localhost/callback" {
		t.Fatalf("redirect URI = %q, want trimmed value", session.RedirectURI)
	}
}

func TestSessionAuthURLRequiresRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		session Session
		want    string
	}{
		{name: "missing redirect", session: Session{State: "state", CodeVerifier: "verifier"}, want: "redirect URI is required"},
		{name: "missing verifier", session: Session{State: "state", RedirectURI: "http://localhost/callback"}, want: "code verifier is required"},
		{name: "missing state", session: Session{CodeVerifier: "verifier", RedirectURI: "http://localhost/callback"}, want: "state is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.session.AuthURL()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("AuthURL() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestExchangeCodeParsesTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm(): %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code_verifier") != "verifier" {
			t.Fatalf("code_verifier = %q", r.Form.Get("code_verifier"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-123",
			"expires_in":    3600,
			"scope":         "openid email profile",
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	oldURL := GoogleTokenURL
	GoogleTokenURL = server.URL
	defer func() { GoogleTokenURL = oldURL }()

	token, err := ExchangeCode(server.Client(), "code-123", "verifier", "http://localhost/callback")
	if err != nil {
		t.Fatalf("ExchangeCode(): %v", err)
	}
	if token.AccessToken != "access-123" {
		t.Fatalf("access token = %q, want access-123", token.AccessToken)
	}
	if token.RefreshToken != "refresh-123" {
		t.Fatalf("refresh token = %q, want refresh-123", token.RefreshToken)
	}
	if len(token.Scopes) != 3 {
		t.Fatalf("len(scopes) = %d, want 3", len(token.Scopes))
	}
	if token.Expiry == 0 {
		t.Fatal("expected expiry to be set")
	}
}

func TestResolveProjectIDReturnsFirstProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-123" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projects": []map[string]string{{"id": "project-1", "name": "Default"}},
		})
	}))
	defer server.Close()

	oldURL := AntigravityProjectsURL
	AntigravityProjectsURL = server.URL
	defer func() { AntigravityProjectsURL = oldURL }()

	projectID, err := ResolveProjectID(server.Client(), "access-123")
	if err != nil {
		t.Fatalf("ResolveProjectID(): %v", err)
	}
	if projectID != "project-1" {
		t.Fatalf("projectID = %q, want project-1", projectID)
	}
}

func TestFetchUserInfoRequiresAccessToken(t *testing.T) {
	_, err := FetchUserInfo(nil, "   ")
	if err == nil || err.Error() != "access token is required" {
		t.Fatalf("FetchUserInfo() error = %v", err)
	}
}

func TestFetchUserInfoRequiresEmailInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"email": ""})
	}))
	defer server.Close()

	oldURL := GoogleUserInfoURL
	GoogleUserInfoURL = server.URL
	defer func() { GoogleUserInfoURL = oldURL }()

	_, err := FetchUserInfo(server.Client(), "access-123")
	if err == nil || err.Error() != "google userinfo response missing email" {
		t.Fatalf("FetchUserInfo() error = %v", err)
	}
}

func TestDoTokenRequestReturnsOAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "token revoked",
		})
	}))
	defer server.Close()

	oldURL := GoogleTokenURL
	GoogleTokenURL = server.URL
	defer func() { GoogleTokenURL = oldURL }()

	_, err := RefreshToken(server.Client(), "refresh-123")
	if err == nil || err.Error() != "oauth error: invalid_grant: token revoked" {
		t.Fatalf("RefreshToken() error = %v", err)
	}
}

func TestDoJSONRequestPropagatesTransportErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})}

	err := doJSONRequest(client, http.MethodGet, "https://example.com", "access-123", nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "request https://example.com") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("doJSONRequest() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
