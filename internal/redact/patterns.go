package redact

import (
	"regexp"
	"strings"
)

const redactedPlaceholder = "[REDACTED]"

var (
	// reBearerBasic matches "Bearer <value>"/"Basic <value>" authorization
	// values. The value runs until the next whitespace or double-quote
	// (rather than a narrow allow-list of characters), so compound tokens
	// that embed "=", ";" or ":" — e.g. a Copilot bearer value shaped like
	// "tid=xxx;exp=yyy;sku=zzz;8kp=1:sig" — are redacted in full instead of
	// only their first segment.
	reBearerBasic = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s"]+`)
	// reOpenAIKey also covers Anthropic ("sk-ant-...") and OpenRouter
	// ("sk-or-...") keys: both are "sk-" followed by an arbitrary prefix and
	// 10+ token characters, which the base pattern already matches.
	reOpenAIKey   = regexp.MustCompile(`\bsk-(?:ant-)?[A-Za-z0-9_-]{10,}\b`)
	reGitHubToken = regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b|\bgithub_pat_[A-Za-z0-9_]{20,}\b`)
	reSlackToken  = regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]+\b`)
	reAgeKey      = regexp.MustCompile(`\bAGE-SECRET-KEY-1[A-Z0-9]+\b`)
	reAWSKey      = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	// reGoogleKey matches Google/Firebase API keys: "AIza" + 35 chars.
	reGoogleKey = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)
	// reGroqKey, reXaiKey, reHFKey match Groq, xAI and HuggingFace tokens by
	// their distinctive prefix.
	reGroqKey = regexp.MustCompile(`\bgsk_[A-Za-z0-9]{10,}\b`)
	reXaiKey  = regexp.MustCompile(`\bxai-[A-Za-z0-9]{10,}\b`)
	reHFKey   = regexp.MustCompile(`\bhf_[A-Za-z0-9]{10,}\b`)
	// reStripeKey matches Stripe-style restricted/secret/publishable live or
	// test keys ("rk_live_...", "sk_test_...", "pk_live_...", ...).
	reStripeKey   = regexp.MustCompile(`\b(?:rk|sk|pk)_(?:live|test)_[A-Za-z0-9]{10,}\b`)
	reJWT         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	reURLUserinfo = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*)://[^\s/@]+:[^\s/@]+@`)

	// reJSONPair matches a `"key":"value"` or escaped `\"key\":\"value\"`
	// pair (the escaped form shows up when a JSON body has been re-encoded
	// as a string, e.g. inside another JSON document or a log line).
	reJSONPair = regexp.MustCompile(`(\\?")([A-Za-z0-9_.\-]+)(\\?")\s*:\s*(\\?")([^"\\]*)(\\?")`)
	// reJSONArrayOrObject matches `"key": [...]` / `"key": {...}` — a JSON
	// pair whose value is a nested array or object rather than a scalar
	// string, e.g. `"api_keys": ["a", "b"]`.
	reJSONArrayOrObject = regexp.MustCompile(`"([A-Za-z0-9_.\-]+)"\s*:\s*(\[[^\]]*\]|\{[^}]*\})`)
	// reColonPair matches YAML/header-style "key: value" pairs, quoted or
	// not: "api_key: secret123", `X-Api-Key: secret123`, `token: "a b"`.
	reColonPair = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_.\-]*)\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\s,;]+))`)
	// reKVPair matches "key=value" pairs, including a double-quoted value
	// that itself contains spaces ("key=\"a b\"").
	reKVPair = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_.\-]*)=(?:"([^"]*)"|([^\s&"]*))`)
)

// String scrubs known secret patterns out of free-form text:
//   - "Bearer <token>" / "Basic <token>" authorization values (full value,
//     including compound tokens with embedded "=", ";" or ":"),
//   - common provider key/token prefixes (OpenAI/Anthropic/OpenRouter
//     "sk-...", GitHub "ghp_.../github_pat_...", Slack "xox?-...", age
//     "AGE-SECRET-KEY-1...", AWS "AKIA...", Google "AIza...", Groq "gsk_...",
//     xAI "xai-...", HuggingFace "hf_...", Stripe-style "rk_live_.../
//     pk_live_..."),
//   - JWTs (three dot-separated base64url segments starting "eyJ"),
//   - URL userinfo ("scheme://user:pass@" becomes "scheme://[REDACTED]@"),
//   - "key=value", `"key":"value"` (plain or backslash-escaped) and
//     "key: value" (YAML/header colon form, quoted or not) pairs whose key
//     looks like a secret, per IsSecretKey — including URL query parameters
//     such as "?key=...", "?api_key=...", "?access_token=...", "?sig=...",
//     "?signature=...".
//
// Every match is replaced with "[REDACTED]" (the value only; the key and
// surrounding punctuation are preserved so the shape of the original text —
// JSON, a query string, a header dump — stays recognizable). Text that
// matches none of the patterns is returned unchanged.
func String(s string) string {
	if s == "" {
		return s
	}
	s = reBearerBasic.ReplaceAllString(s, "$1 "+redactedPlaceholder)
	s = reOpenAIKey.ReplaceAllString(s, redactedPlaceholder)
	s = reGitHubToken.ReplaceAllString(s, redactedPlaceholder)
	s = reSlackToken.ReplaceAllString(s, redactedPlaceholder)
	s = reAgeKey.ReplaceAllString(s, redactedPlaceholder)
	s = reAWSKey.ReplaceAllString(s, redactedPlaceholder)
	s = reGoogleKey.ReplaceAllString(s, redactedPlaceholder)
	s = reGroqKey.ReplaceAllString(s, redactedPlaceholder)
	s = reXaiKey.ReplaceAllString(s, redactedPlaceholder)
	s = reHFKey.ReplaceAllString(s, redactedPlaceholder)
	s = reStripeKey.ReplaceAllString(s, redactedPlaceholder)
	s = reJWT.ReplaceAllString(s, redactedPlaceholder)
	s = reURLUserinfo.ReplaceAllString(s, "$1://"+redactedPlaceholder+"@")
	s = redactJSONPairs(s)
	s = redactJSONArrayPairs(s)
	s = redactColonPairs(s)
	s = redactKVPairs(s)
	return s
}

// redactJSONPairs replaces the value half of every `"key":"value"` (or
// backslash-escaped `\"key\":\"value\"`) pair whose key is a secret,
// preserving whichever quote style (plain or escaped) was actually used.
func redactJSONPairs(s string) string {
	return reJSONPair.ReplaceAllStringFunc(s, func(m string) string {
		sub := reJSONPair.FindStringSubmatch(m)
		if len(sub) != 7 {
			return m
		}
		openKeyQ, key, closeKeyQ, openValQ, val, closeValQ := sub[1], sub[2], sub[3], sub[4], sub[5], sub[6]
		if val == "" || !IsSecretKey(key) {
			return m
		}
		return openKeyQ + key + closeKeyQ + ":" + openValQ + redactedPlaceholder + closeValQ
	})
}

// redactJSONArrayPairs replaces the value of a `"key": [...]` / `"key":
// {...}` pair whose key is a secret with a plain redacted string, so a
// secret shipped as a JSON array/object value (e.g. `"api_keys": ["a"]`) is
// scrubbed the same as a scalar one.
func redactJSONArrayPairs(s string) string {
	return reJSONArrayOrObject.ReplaceAllStringFunc(s, func(m string) string {
		sub := reJSONArrayOrObject.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		key := sub[1]
		if !IsSecretKey(key) {
			return m
		}
		return `"` + key + `":"` + redactedPlaceholder + `"`
	})
}

// redactColonPairs replaces the value half of every YAML/header-style "key:
// value" pair whose key is a secret. The value may be unquoted, double- or
// single-quoted (quoted values may contain spaces).
func redactColonPairs(s string) string {
	return reColonPair.ReplaceAllStringFunc(s, func(m string) string {
		sub := reColonPair.FindStringSubmatch(m)
		if len(sub) != 5 {
			return m
		}
		key, dq, sq, unq := sub[1], sub[2], sub[3], sub[4]
		if !IsSecretKey(key) {
			return m
		}
		// "Authorization: Bearer <token>" (and "Basic") is already fully
		// handled by reBearerBasic above, which redacts the actual token
		// after the scheme keyword. Without this guard, the colon-pair
		// pattern would additionally treat the bare scheme word itself as
		// "the value" for key "Authorization" and redact it too, destroying
		// the "Bearer "/"Basic " prefix the earlier pass deliberately kept.
		if strings.EqualFold(unq, "Bearer") || strings.EqualFold(unq, "Basic") {
			return m
		}
		switch {
		case dq != "":
			return key + `: "` + redactedPlaceholder + `"`
		case sq != "":
			return key + `: '` + redactedPlaceholder + `'`
		case unq != "":
			return key + ": " + redactedPlaceholder
		default:
			// Empty value (e.g. "key: " with nothing after it): leave as is,
			// same convention as redactKVPairs, so an unset field does not
			// turn into a misleading "[REDACTED]".
			return m
		}
	})
}

// redactKVPairs replaces the value half of every "key=value" pair whose key
// is a secret. The value may be unquoted (stops at whitespace/"&", so
// several "k=v&k2=v2" query-string pairs on one line are handled
// individually) or double-quoted (stops only at the closing quote, so a
// quoted value containing spaces is redacted in full rather than only up to
// its first word).
func redactKVPairs(s string) string {
	return reKVPair.ReplaceAllStringFunc(s, func(m string) string {
		sub := reKVPair.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		key, quoted, unquoted := sub[1], sub[2], sub[3]
		if !IsSecretKey(key) {
			return m
		}
		// Distinguish "key=\"...\"" (quoted alternative matched, even an
		// empty string) from "key=..." (unquoted) by checking what actually
		// follows "key=" in the raw match, since an empty capture is
		// ambiguous between "the quoted alt matched an empty string" and
		// "the quoted alt did not participate".
		rest := m[len(key)+1:]
		if strings.HasPrefix(rest, `"`) {
			if quoted == "" {
				return m
			}
			return key + `="` + redactedPlaceholder + `"`
		}
		if unquoted == "" {
			return m
		}
		return key + "=" + redactedPlaceholder
	})
}
