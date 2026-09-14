package cmd

import (
	"strings"
	"testing"
)

func TestEnsureMCPHTTPToken_ConfiguredTokenIsUsedAsIs(t *testing.T) {
	token, generated, err := ensureMCPHTTPToken("localhost", "configured-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generated {
		t.Error("generated = true, want false for an explicitly configured token")
	}
	if token != "configured-token" {
		t.Errorf("token = %q, want the configured value unchanged", token)
	}
}

func TestEnsureMCPHTTPToken_LoopbackWithNoTokenGeneratesOne(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "  localhost  "} {
		t.Run(host, func(t *testing.T) {
			token, generated, err := ensureMCPHTTPToken(host, "")
			if err != nil {
				t.Fatalf("unexpected error for loopback host %q: %v", host, err)
			}
			if !generated {
				t.Error("generated = false, want true when no token is configured")
			}
			if len(token) != 64 { // 32 random bytes, hex-encoded
				t.Errorf("token length = %d, want 64 hex characters", len(token))
			}
		})
	}
}

func TestEnsureMCPHTTPToken_NonLoopbackWithNoTokenIsRefused(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "", "192.168.1.10", "example.com", "::"} {
		t.Run(host, func(t *testing.T) {
			_, _, err := ensureMCPHTTPToken(host, "")
			if err == nil {
				t.Fatalf("expected an error for non-loopback host %q with no token configured", host)
			}
			if !strings.Contains(err.Error(), "MCPServer.HttpToken") {
				t.Errorf("error %q does not name the config key MCPServer.HttpToken", err.Error())
			}
		})
	}
}

func TestEnsureMCPHTTPToken_NonLoopbackWithConfiguredTokenSucceeds(t *testing.T) {
	// A configured token is honored even on a non-loopback bind: the refusal
	// only guards against starting with NO authentication at all.
	token, generated, err := ensureMCPHTTPToken("0.0.0.0", "configured-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generated {
		t.Error("generated = true, want false for an explicitly configured token")
	}
	if token != "configured-token" {
		t.Errorf("token = %q, want configured-token", token)
	}
}

func TestEnsureMCPHTTPToken_GeneratedTokensAreNotIdentical(t *testing.T) {
	tokenA, _, err := ensureMCPHTTPToken("localhost", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tokenB, _, err := ensureMCPHTTPToken("localhost", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenA == tokenB {
		t.Fatal("two independently generated tokens must not collide")
	}
}

func TestIsLoopbackMCPHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":   true,
		"LOCALHOST":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"[::1]":       true,
		"0.0.0.0":     false,
		"::":          false,
		"":            false,
		"example.com": false,
		"192.168.1.5": false,
	}
	for host, want := range cases {
		host, want := host, want
		t.Run(host, func(t *testing.T) {
			if got := isLoopbackMCPHost(host); got != want {
				t.Errorf("isLoopbackMCPHost(%q) = %v, want %v", host, got, want)
			}
		})
	}
}
