package telemetry

import "testing"

func TestTokenEnvOverride(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()
	sourceToken = "built-in-token"

	t.Setenv("PANDO_TELEMETRY_TOKEN", "env-token")
	if got := Token(); got != "env-token" {
		t.Fatalf("Token() = %q, want %q", got, "env-token")
	}
}

func TestTokenFallsBackToBuildValue(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()
	sourceToken = "built-in-token"

	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	if got := Token(); got != "built-in-token" {
		t.Fatalf("Token() = %q, want %q", got, "built-in-token")
	}
}

func TestEndpointDefault(t *testing.T) {
	original := ingestHost
	defer func() { ingestHost = original }()
	ingestHost = "example.betterstackdata.com"

	t.Setenv("PANDO_TELEMETRY_ENDPOINT", "")
	want := "https://example.betterstackdata.com"
	if got := Endpoint(); got != want {
		t.Fatalf("Endpoint() = %q, want %q", got, want)
	}
}

func TestEndpointEnvOverrideAllowsHTTP(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", "http://127.0.0.1:9999/ingest")
	want := "http://127.0.0.1:9999/ingest"
	if got := Endpoint(); got != want {
		t.Fatalf("Endpoint() = %q, want %q", got, want)
	}
}

// TestTokenNeverFallsBackToBuiltInForCustomEndpoint verifies the code-review
// fix: a custom PANDO_TELEMETRY_ENDPOINT must never cause the build-time
// source token to be used, even when PANDO_TELEMETRY_TOKEN is unset — that
// secret is scoped to the official ingest host only.
func TestTokenNeverFallsBackToBuiltInForCustomEndpoint(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()
	sourceToken = "built-in-token"

	t.Setenv("PANDO_TELEMETRY_ENDPOINT", "https://example.com/ingest")
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	if got := Token(); got != "" {
		t.Fatalf("Token() = %q, want empty (custom endpoint must not use the built-in token)", got)
	}

	// A custom endpoint WITH its own token still works normally (this is
	// the Phase 6 E2E suite's exact configuration).
	t.Setenv("PANDO_TELEMETRY_TOKEN", "custom-token")
	if got := Token(); got != "custom-token" {
		t.Fatalf("Token() = %q, want %q", got, "custom-token")
	}
}

// TestAvailableRequiresSecureOrLoopbackForCustomEndpoint verifies the
// code-review fix: a custom endpoint must be https, or plain http only to a
// loopback address — anything else (plain http to a real host) must report
// Available() = false rather than send the token/records in the clear.
func TestAvailableRequiresSecureOrLoopbackForCustomEndpoint(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()
	sourceToken = "" // force reliance on PANDO_TELEMETRY_TOKEN, like a custom endpoint requires anyway
	t.Setenv("PANDO_TELEMETRY_TOKEN", "custom-token")

	cases := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{"https to a real host", "https://example.betterstackdata.com", true},
		{"http to 127.0.0.1 (Phase 6 E2E's exact shape)", "http://127.0.0.1:9999/ingest", true},
		{"http to localhost", "http://localhost:9999/ingest", true},
		{"http to ::1", "http://[::1]:9999/ingest", true},
		{"http to a real host is rejected", "http://example.com/ingest", false},
		{"http to a non-loopback IP is rejected", "http://10.0.0.5:9999/ingest", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PANDO_TELEMETRY_ENDPOINT", c.endpoint)
			if got := Available(); got != c.want {
				t.Errorf("Available() with endpoint %q = %v, want %v", c.endpoint, got, c.want)
			}
		})
	}
}

// TestAvailableDefaultEndpointUnaffectedByHTTPSRule verifies the https/
// loopback rule is scoped to a CUSTOM endpoint only: an unmodified build
// (no PANDO_TELEMETRY_ENDPOINT) is available from just a token, since
// Endpoint() always returns https for the default host.
func TestAvailableDefaultEndpointUnaffectedByHTTPSRule(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()
	sourceToken = "built-in-token"
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", "")
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")

	if !Available() {
		t.Fatal("Available() = false, want true for the default endpoint with a build-time token")
	}
}

func TestAvailable(t *testing.T) {
	original := sourceToken
	defer func() { sourceToken = original }()

	sourceToken = ""
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	if Available() {
		t.Fatal("Available() = true, want false with no token set anywhere")
	}

	sourceToken = "built-in-token"
	if !Available() {
		t.Fatal("Available() = false, want true with a build-time token")
	}
}
