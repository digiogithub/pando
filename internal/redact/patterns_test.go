package redact

import (
	"strings"
	"testing"
)

// fakeSecret joins its parts at runtime. Provider-shaped test fixtures are
// always split this way so no contiguous token literal appears in source,
// which would otherwise trip secret scanners such as GitHub push protection.
func fakeSecret(parts ...string) string {
	return strings.Join(parts, "")
}

func TestStringRedactsKnownPatterns(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bearer token",
			input: "calling with header Authorization: Bearer abcDEF123.456-789",
			want:  "calling with header Authorization: Bearer [REDACTED]",
		},
		{
			name:  "basic auth",
			input: "Authorization: Basic dXNlcjpwYXNz",
			want:  "Authorization: Basic [REDACTED]",
		},
		{
			name:  "openai key",
			input: "using key sk-abcdefghijklmnop for the request",
			want:  "using key [REDACTED] for the request",
		},
		{
			name:  "anthropic key",
			input: "token=" + fakeSecret("sk-", "ant-", "api03-abcdefghijklmnopqrstuvwxyz"),
			want:  "token=[REDACTED]",
		},
		{
			name:  "github pat",
			input: "clone with " + fakeSecret("gh", "p_", "1234567890abcdefghijklmnopqrstuvwx"),
			want:  "clone with [REDACTED]",
		},
		{
			name:  "github fine-grained pat",
			input: "clone with " + fakeSecret("github", "_pat_", "1234567890abcdefghijklmnop"),
			want:  "clone with [REDACTED]",
		},
		{
			name:  "slack token",
			input: "posting with " + fakeSecret("xo", "xb-", "1234567890-abcdefghijklmnop"),
			want:  "posting with [REDACTED]",
		},
		{
			name:  "age secret key",
			input: fakeSecret("AGE-SECRET-", "KEY-1", "QGX8ZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQZQ"),
			want:  "[REDACTED]",
		},
		{
			name:  "aws access key",
			input: "AWS_ACCESS_KEY_ID=" + fakeSecret("AK", "IA", "IOSFODNN7EXAMPLE"),
			want:  "AWS_ACCESS_KEY_ID=[REDACTED]",
		},
		{
			name:  "jwt",
			input: "cookie=" + fakeSecret("ey", "JhbGciOiJIUzI1NiJ9.", "ey", "JzdWIiOiIxMjM0NTY3ODkwIn0.", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"),
			want:  "cookie=[REDACTED]",
		},
		{
			name:  "url userinfo",
			input: "postgres://admin:s3cr3t@db.internal:5432/pando",
			want:  "postgres://[REDACTED]@db.internal:5432/pando",
		},
		{
			name:  "json pair secret key",
			input: `{"apiKey":"sk-live-1234567890","baseUrl":"https://api.example.com"}`,
			want:  `{"apiKey":"[REDACTED]","baseUrl":"https://api.example.com"}`,
		},
		{
			name:  "kv pair secret key",
			input: "PANDO_API_KEY=abcdef123456 DEBUG=1",
			want:  "PANDO_API_KEY=[REDACTED] DEBUG=1",
		},
		{
			name:  "unrelated text untouched",
			input: "hello world, this is a normal log line",
			want:  "hello world, this is a normal log line",
		},
		{
			name:  "non secret kv untouched",
			input: "count=42 name=pando",
			want:  "count=42 name=pando",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := String(tc.input); got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestStringEmpty(t *testing.T) {
	if got := String(""); got != "" {
		t.Errorf("String(\"\") = %q, want empty", got)
	}
}

// TestStringRedactsReviewGaps covers the specific gaps flagged in code
// review: colon forms (header dumps, YAML), escaped JSON, quoted values with
// spaces, new key suffixes (passphrase/pwd/api_keys), new provider token
// prefixes (Google/Groq/xAI/HuggingFace/Stripe-like), a Copilot-style
// multi-segment bearer value, and URL query-string secrets — plus negatives
// proving ordinary words are never touched.
func TestStringRedactsReviewGaps(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "header colon form",
			in:   "X-Api-Key: plainkey123",
			want: "X-Api-Key: [REDACTED]",
		},
		{
			name: "yaml colon form",
			in:   "api_key: yamlsecret123",
			want: "api_key: [REDACTED]",
		},
		{
			name: "escaped json pair",
			in:   `{\"api_key\":\"escapedsecret\"}`,
			want: `{\"api_key\":\"[REDACTED]\"}`,
		},
		{
			name: "quoted value with space",
			in:   `token="a b"`,
			want: `token="[REDACTED]"`,
		},
		{
			name: "passphrase and pwd",
			in:   "passphrase=hunter2 pwd=hunter3",
			want: "passphrase=[REDACTED] pwd=[REDACTED]",
		},
		{
			name: "plural api_keys json array value",
			in:   `"api_keys": ["listsecret"]`,
			want: `"api_keys":"[REDACTED]"`,
		},
		{
			name: "copilot compound bearer value fully redacted",
			in:   "Authorization: Bearer tid=abc;exp=1;sku=x;8kp=1:deadbeefcafe",
			want: "Authorization: Bearer [REDACTED]",
		},
		{
			name: "google api key",
			in:   "key is " + fakeSecret("AI", "za", "SyA1234567890abcdefghijklmnopqrstuv"),
			want: "key is [REDACTED]",
		},
		{
			name: "groq key",
			in:   fakeSecret("gs", "k_", "ABCDEFGHIJKLMNOPQRSTUV"),
			want: "[REDACTED]",
		},
		{
			name: "xai key",
			in:   fakeSecret("xa", "i-", "ABCDEFGHIJKLMNOP"),
			want: "[REDACTED]",
		},
		{
			name: "huggingface key",
			in:   fakeSecret("h", "f_", "ABCDEFGHIJKLMNOPQRS"),
			want: "[REDACTED]",
		},
		{
			name: "stripe-like restricted key",
			in:   fakeSecret("rk", "_live_", "ABCDEFGHIJKLMNOP"),
			want: "[REDACTED]",
		},
		{
			name: "url query secret params",
			in:   "https://x/?key=AIzaSyXYZ&alt=sse&sig=abcdef&token=zzz",
			want: "https://x/?key=[REDACTED]&alt=sse&sig=[REDACTED]&token=[REDACTED]",
		},
		{
			name: "negative: tokens used prose",
			in:   "tokens used: 123",
			want: "tokens used: 123",
		},
		{
			name: "negative: keyboard word",
			in:   "keyboard shortcuts enabled",
			want: "keyboard shortcuts enabled",
		},
		{
			name: "negative: prompt token counters",
			in:   "promptTokens=150 maxTokens=4096",
			want: "promptTokens=150 maxTokens=4096",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := String(tc.in); got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
