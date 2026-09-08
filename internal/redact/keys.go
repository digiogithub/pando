// Package redact provides key- and value-based scrubbing of secrets (API
// keys, tokens, passwords, cookies, credentials...) from configuration
// trees, HTTP headers, log attributes and free-form text. It has no
// dependencies beyond the standard library, so any package — including ones
// that must not import internal/config or internal/logging — can use it.
package redact

import "strings"

// secretKeySuffixes are the lowercase suffixes that mark a configuration or
// log-attribute key as holding a credential. Matching is suffix-based, so
// separators before the suffix never matter: "api_key", "apiKey" and
// "API-KEY" all end in "key" once lowercased and match alike, without any
// need to special-case snake_case, camelCase or kebab-case individually.
// A few entries (apikey, api_key, private_key) are listed anyway for
// documentation even though the bare "key" suffix already covers them.
//
// Deliberately NOT included: a bare "keys" or "tokens" plural suffix. This
// codebase has many pervasive, genuinely non-secret counter/list fields that
// would collide with those — "promptTokens", "maxTokens", "inputTokens",
// "outputTokens", "totalTokens", "cacheReadTokens", "dailyModelTokens" (all
// token *counts*, never credentials) being the concrete, already
// unit-tested example (see
// internal/llm/tools.TestRedactSetupValueMasksSecretsOnly's "promptTokens
// stays unmasked" assertion). "api_keys"/"apikeys" is added explicitly
// instead, since a list of API keys has no equally common non-secret
// homonym in this codebase.
var secretKeySuffixes = []string{
	"key", "apikey", "api_key", "private_key",
	"apikeys", "api_keys",
	"token",
	"secret", "secrets",
	"password", "passwd",
	"passphrase", "pwd",
	"authorization",
	"cookie",
	"credential", "credentials",
	"sig", "signature",
}

// IsSecretKey reports whether key names a value that must never be logged or
// shipped in the clear: an API key, access token, password, cookie,
// authorization header or generic credential. Matching is case-insensitive
// and suffix-based, so "apiKey", "api_key", "API-KEY", "oauthAccessToken"
// and "Authorization" all match, while plural counters such as
// "promptTokens" or unrelated words such as "tokenOptimization" do not
// (neither ends in the singular "token" suffix).
func IsSecretKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, suffix := range secretKeySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
