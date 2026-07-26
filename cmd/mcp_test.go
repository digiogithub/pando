package cmd

import "testing"

func TestParseManualCodeInput_FullURLWithState(t *testing.T) {
	code, state, iss, wasBareCode, err := parseManualCodeInput(
		"http://127.0.0.1:19876/mcp/oauth/callback?code=abc123&state=xyz789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasBareCode {
		t.Errorf("wasBareCode = true, want false for a full URL")
	}
	if code != "abc123" {
		t.Errorf("code = %q, want abc123", code)
	}
	if state != "xyz789" {
		t.Errorf("state = %q, want xyz789", state)
	}
	if iss != "" {
		t.Errorf("iss = %q, want empty (no iss param present)", iss)
	}
}

func TestParseManualCodeInput_FullURLWithIss(t *testing.T) {
	code, state, iss, wasBareCode, err := parseManualCodeInput(
		"http://127.0.0.1:19876/mcp/oauth/callback?code=abc123&state=xyz789&iss=https%3A%2F%2Fas.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasBareCode {
		t.Errorf("wasBareCode = true, want false")
	}
	if iss != "https://as.example.com" {
		t.Errorf("iss = %q, want https://as.example.com", iss)
	}
	_ = code
	_ = state
}

func TestParseManualCodeInput_FullURLMissingStateIsRejected(t *testing.T) {
	_, _, _, _, err := parseManualCodeInput(
		"http://127.0.0.1:19876/mcp/oauth/callback?code=abc123")
	if err == nil {
		t.Fatal("expected an error when a full redirect URL is missing its state parameter")
	}
}

func TestParseManualCodeInput_FullURLMissingCode(t *testing.T) {
	_, _, _, _, err := parseManualCodeInput(
		"http://127.0.0.1:19876/mcp/oauth/callback?state=xyz789")
	if err == nil {
		t.Fatal("expected an error when a full redirect URL is missing its code parameter")
	}
}

func TestParseManualCodeInput_FullURLWithErrorParam(t *testing.T) {
	_, _, _, _, err := parseManualCodeInput(
		"http://127.0.0.1:19876/mcp/oauth/callback?error=access_denied&error_description=user+declined&state=xyz789")
	if err == nil {
		t.Fatal("expected an error when the redirect URL carries an OAuth error")
	}
}

func TestParseManualCodeInput_BareCode(t *testing.T) {
	code, state, iss, wasBareCode, err := parseManualCodeInput("just-a-bare-authorization-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasBareCode {
		t.Errorf("wasBareCode = false, want true for non-URL input")
	}
	if code != "just-a-bare-authorization-code" {
		t.Errorf("code = %q, want the raw input", code)
	}
	if state != "" {
		t.Errorf("state = %q, want empty for a bare code", state)
	}
	if iss != "" {
		t.Errorf("iss = %q, want empty for a bare code", iss)
	}
}

func TestParseManualCodeInput_EmptyInput(t *testing.T) {
	_, _, _, _, err := parseManualCodeInput("")
	if err == nil {
		t.Fatal("expected an error for empty input")
	}
}
