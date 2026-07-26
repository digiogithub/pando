package mcpauth

import (
	"reflect"
	"testing"
)

func TestParseChallenge_SimpleBearer(t *testing.T) {
	c := ParseChallenge(`Bearer error="insufficient_scope", error_description="more scope required", scope="read write"`)
	if c.Scheme != "Bearer" {
		t.Errorf("Scheme = %q, want Bearer", c.Scheme)
	}
	if c.Error != "insufficient_scope" {
		t.Errorf("Error = %q, want insufficient_scope", c.Error)
	}
	if c.ErrorDescription != "more scope required" {
		t.Errorf("ErrorDescription = %q, want %q", c.ErrorDescription, "more scope required")
	}
	if !reflect.DeepEqual(c.Scope, []string{"read", "write"}) {
		t.Errorf("Scope = %v, want [read write]", c.Scope)
	}
}

func TestParseChallenge_BareSchemeNoParams(t *testing.T) {
	c := ParseChallenge("Bearer")
	if c.Scheme != "Bearer" {
		t.Errorf("Scheme = %q, want Bearer", c.Scheme)
	}
	if c.Error != "" || len(c.Scope) != 0 {
		t.Errorf("expected no params, got %+v", c)
	}
}

func TestParseChallenge_ResourceMetadata(t *testing.T) {
	c := ParseChallenge(`Bearer resource_metadata="https://res.example.com/.well-known/oauth-protected-resource"`)
	if c.ResourceMetadata != "https://res.example.com/.well-known/oauth-protected-resource" {
		t.Errorf("ResourceMetadata = %q", c.ResourceMetadata)
	}
}

func TestParseChallenge_UnquotedValues(t *testing.T) {
	c := ParseChallenge(`Bearer error=insufficient_scope`)
	if c.Error != "insufficient_scope" {
		t.Errorf("Error = %q, want insufficient_scope (unquoted value)", c.Error)
	}
}

func TestParseChallenge_MultipleChallengesOnOneLine_PrefersBearer(t *testing.T) {
	c := ParseChallenge(`Basic realm="test", Bearer error="insufficient_scope", scope="admin"`)
	if c.Scheme != "Bearer" {
		t.Fatalf("Scheme = %q, want Bearer (preferred over Basic)", c.Scheme)
	}
	if c.Error != "insufficient_scope" {
		t.Errorf("Error = %q, want insufficient_scope", c.Error)
	}
	if !reflect.DeepEqual(c.Scope, []string{"admin"}) {
		t.Errorf("Scope = %v, want [admin]", c.Scope)
	}
}

func TestParseChallenge_MultipleChallengesOnOneLine_BearerFirst(t *testing.T) {
	c := ParseChallenge(`Bearer error="insufficient_scope", scope="admin", Basic realm="test"`)
	if c.Scheme != "Bearer" {
		t.Fatalf("Scheme = %q, want Bearer", c.Scheme)
	}
	if c.Error != "insufficient_scope" {
		t.Errorf("Error = %q, want insufficient_scope", c.Error)
	}
	// The trailing Basic challenge's realm param must not leak into the
	// Bearer challenge's fields.
	if c.ResourceMetadata != "" {
		t.Errorf("ResourceMetadata = %q, want empty (realm isn't modeled)", c.ResourceMetadata)
	}
}

func TestParseChallenge_NoBearerReturnsFirst(t *testing.T) {
	c := ParseChallenge(`Basic realm="test"`)
	if c.Scheme != "Basic" {
		t.Errorf("Scheme = %q, want Basic (no Bearer present, fall back to first)", c.Scheme)
	}
}

func TestParseChallenge_Empty(t *testing.T) {
	c := ParseChallenge("")
	if c.Scheme != "" {
		t.Errorf("Scheme = %q, want empty for empty header", c.Scheme)
	}
}

func TestParseChallenge_CommaInsideQuotedValueNotSplit(t *testing.T) {
	c := ParseChallenge(`Bearer error="insufficient_scope", error_description="access denied, try again", scope="a b"`)
	if c.ErrorDescription != "access denied, try again" {
		t.Errorf("ErrorDescription = %q, want %q", c.ErrorDescription, "access denied, try again")
	}
	if !reflect.DeepEqual(c.Scope, []string{"a", "b"}) {
		t.Errorf("Scope = %v, want [a b]", c.Scope)
	}
}

func TestParseChallenges_AcrossMultipleHeaderLines(t *testing.T) {
	headers := []string{
		`Basic realm="test"`,
		`Bearer error="insufficient_scope", scope="a b c"`,
	}
	c := ParseChallenges(headers)
	if c.Scheme != "Bearer" {
		t.Fatalf("Scheme = %q, want Bearer", c.Scheme)
	}
	if !reflect.DeepEqual(c.Scope, []string{"a", "b", "c"}) {
		t.Errorf("Scope = %v, want [a b c]", c.Scope)
	}
}

func TestParseChallenges_Empty(t *testing.T) {
	c := ParseChallenges(nil)
	if c.Scheme != "" {
		t.Errorf("Scheme = %q, want empty", c.Scheme)
	}
}

func TestParseChallenge_MissingParamsGraceful(t *testing.T) {
	c := ParseChallenge(`Bearer realm="test"`)
	if c.Scheme != "Bearer" {
		t.Errorf("Scheme = %q, want Bearer", c.Scheme)
	}
	if c.Error != "" || c.ErrorDescription != "" || len(c.Scope) != 0 || c.ResourceMetadata != "" {
		t.Errorf("expected all modeled fields empty for an unrecognized param, got %+v", c)
	}
}

func TestParseChallenge_EscapedQuoteInValue(t *testing.T) {
	c := ParseChallenge(`Bearer error_description="she said \"hi\""`)
	if c.ErrorDescription != `she said "hi"` {
		t.Errorf("ErrorDescription = %q, want %q", c.ErrorDescription, `she said "hi"`)
	}
}
