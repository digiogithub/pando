package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateIssuer_Table(t *testing.T) {
	const expected = "https://as.example.com"

	tests := []struct {
		name      string
		got       string
		supported bool
		wantErr   error // nil means "no error"
	}{
		{
			name:      "supported true, exact match",
			got:       expected,
			supported: true,
			wantErr:   nil,
		},
		{
			name:      "supported true, iss absent -> reject",
			got:       "",
			supported: true,
			wantErr:   errIssMissing,
		},
		{
			name:      "supported true, iss mismatched -> reject",
			got:       "https://attacker.example.com",
			supported: true,
			wantErr:   errIssMismatch,
		},
		{
			name:      "supported false, iss absent -> proceed",
			got:       "",
			supported: false,
			wantErr:   nil,
		},
		{
			name:      "supported false, iss present and matches -> proceed",
			got:       expected,
			supported: false,
			wantErr:   nil,
		},
		{
			name:      "supported false, iss present and mismatched -> reject (compare anyway)",
			got:       "https://attacker.example.com",
			supported: false,
			wantErr:   errIssMismatch,
		},
		// RFC 3986 §6.2.1 simple string comparison: none of the following
		// "equivalent under a lenient URL parser" variants may compare
		// equal. All must be treated as mismatches regardless of
		// `supported`.
		{
			name:      "trailing slash difference is a mismatch",
			got:       expected + "/",
			supported: true,
			wantErr:   errIssMismatch,
		},
		{
			name:      "case difference in scheme/host is a mismatch",
			got:       "HTTPS://AS.EXAMPLE.COM",
			supported: true,
			wantErr:   errIssMismatch,
		},
		{
			name:      "default port elision is a mismatch",
			got:       expected + ":443",
			supported: true,
			wantErr:   errIssMismatch,
		},
		{
			name:      "percent-encoding difference is a mismatch",
			got:       "https://as.example.com%2F",
			supported: true,
			wantErr:   errIssMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIssuer(expected, tt.got, tt.supported)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validateIssuer(%q, %q, %v) = %v, want nil", expected, tt.got, tt.supported, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateIssuer(%q, %q, %v) = nil, want error wrapping %v", expected, tt.got, tt.supported, tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateIssuer(%q, %q, %v) = %v, want it to wrap %v", expected, tt.got, tt.supported, err, tt.wantErr)
			}
		})
	}
}

func TestFetchIssSupport_ExplicitURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://as.example.com",
			"authorization_response_iss_parameter_supported": true,
		})
	}))
	defer srv.Close()

	got := fetchIssSupport(context.Background(), srv.Client(), srv.URL, "https://as.example.com")
	if !got {
		t.Errorf("fetchIssSupport = false, want true")
	}
}

func TestFetchIssSupport_FieldAbsentDefaultsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": "https://as.example.com"})
	}))
	defer srv.Close()

	got := fetchIssSupport(context.Background(), srv.Client(), srv.URL, "https://as.example.com")
	if got {
		t.Errorf("fetchIssSupport = true, want false when the field is absent")
	}
}

func TestFetchIssSupport_NetworkFailureDegradesToFalse(t *testing.T) {
	got := fetchIssSupport(context.Background(), http.DefaultClient, "http://127.0.0.1:1/does-not-exist", "https://as.example.com")
	if got {
		t.Errorf("fetchIssSupport = true, want false on network failure")
	}
}

func TestFetchIssSupport_DiscoveryFallbackFromIssuer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_response_iss_parameter_supported": true,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// No explicit metadataURL: fetchIssSupport must derive the well-known
	// discovery URL from the issuer itself, the same way mcp-go's own
	// auto-discovery does for an issuer with no path component.
	got := fetchIssSupport(context.Background(), srv.Client(), "", srv.URL)
	if !got {
		t.Errorf("fetchIssSupport via discovery = false, want true")
	}
}

func TestIssDiscoveryCandidateURLs_NoPath(t *testing.T) {
	urls := issDiscoveryCandidateURLs("https://as.example.com")
	want := []string{
		"https://as.example.com/.well-known/oauth-authorization-server",
		"https://as.example.com/.well-known/openid-configuration",
	}
	if len(urls) != len(want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestIssDiscoveryCandidateURLs_WithPath(t *testing.T) {
	urls := issDiscoveryCandidateURLs("https://as.example.com/tenant1")
	want := []string{
		"https://as.example.com/.well-known/oauth-authorization-server/tenant1",
		"https://as.example.com/.well-known/openid-configuration/tenant1",
		"https://as.example.com/tenant1/.well-known/openid-configuration",
	}
	if len(urls) != len(want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestIssDiscoveryCandidateURLs_Unparseable(t *testing.T) {
	if urls := issDiscoveryCandidateURLs("::not a url"); urls != nil {
		t.Errorf("urls = %v, want nil", urls)
	}
}
