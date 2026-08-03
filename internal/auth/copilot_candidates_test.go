package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateTokenSources points the config dir at a temp dir and clears every
// token env var, so these tests never pick up the developer's real credentials.
func isolateTokenSources(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	for _, key := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST"} {
		t.Setenv(key, "")
	}
	return dir
}

func TestGitHubOAuthTokenCandidatesOrderAndDedup(t *testing.T) {
	dir := isolateTokenSources(t)
	t.Setenv("COPILOT_GITHUB_TOKEN", "gho_explicit")
	t.Setenv("GITHUB_TOKEN", "gho_explicit") // duplicate, must be dropped

	appsDir := filepath.Join(dir, "github-copilot")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `{
		"github.com:Iv1.aaa": {"oauth_token": "ghu_editor_a", "user": "octocat"},
		"github.com:Ov23.bbb": {"oauth_token": "gho_editor_b", "user": "octocat"},
		"ghe.example.com:Iv1.ccc": {"oauth_token": "ghu_other_host"}
	}`
	if err := os.WriteFile(filepath.Join(appsDir, "apps.json"), []byte(apps), 0o600); err != nil {
		t.Fatal(err)
	}

	candidates := GitHubOAuthTokenCandidates()
	var got []string
	for _, c := range candidates {
		got = append(got, c.Token)
	}

	want := []string{"gho_explicit", "ghu_editor_a", "gho_editor_b"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
	if candidates[0].Source != "COPILOT_GITHUB_TOKEN" {
		t.Errorf("first source = %q", candidates[0].Source)
	}
	if candidates[1].Source != "github-copilot-app" {
		t.Errorf("second source = %q", candidates[1].Source)
	}
}

func TestLoadGitHubOAuthTokenPrefersFirstCandidate(t *testing.T) {
	isolateTokenSources(t)
	t.Setenv("COPILOT_GITHUB_TOKEN", "gho_first")
	t.Setenv("GH_TOKEN", "gho_second")

	token, err := LoadGitHubOAuthToken()
	if err != nil {
		t.Fatalf("LoadGitHubOAuthToken: %v", err)
	}
	if token != "gho_first" {
		t.Errorf("token = %q, want gho_first", token)
	}
}

func TestLoadGitHubOAuthTokenWithoutSources(t *testing.T) {
	isolateTokenSources(t)
	if _, err := LoadGitHubOAuthToken(); err == nil {
		t.Fatal("expected an error when no token source exists")
	}
}

func TestLoadGitHubOAuthTokenRejectsPAT(t *testing.T) {
	isolateTokenSources(t)
	t.Setenv("GITHUB_TOKEN", "ghp_personal_access_token")

	if _, err := LoadGitHubOAuthToken(); err == nil {
		t.Fatal("PAT must not be accepted as a Copilot token")
	}
}
