package mcpauth

import "strings"

// Challenge is a parsed WWW-Authenticate challenge (RFC 7235 §4.1), extended
// with the two parameters the MCP authorization spec cares about: the RFC
// 6750 §3.1 "insufficient_scope" error (with its space-separated "scope"
// value) and the RFC 9728 §5.1 "resource_metadata" parameter.
type Challenge struct {
	Scheme           string
	Error            string
	ErrorDescription string
	Scope            []string
	ResourceMetadata string
}

// ParseChallenge parses a WWW-Authenticate header value into a single
// Challenge. The header value may itself contain more than one challenge —
// RFC 7235's "1#challenge" grammar allows several challenges separated by
// top-level commas in one header line, and RFC 7230 §3.2.2 separately
// allows a header field to be repeated across multiple lines (which HTTP
// semantics permit combining into one comma-joined value). Callers with
// several raw header lines (resp.Header.Values("WWW-Authenticate")) should
// join them with ", " before calling ParseChallenge, or call it once per
// line and keep whichever result has Scheme=="Bearer" — see ParseChallenges
// for a helper that does this automatically.
//
// When the header describes several challenges, ParseChallenge prefers the
// "Bearer" scheme challenge (the one the MCP OAuth flows and step-up
// re-authorization care about); if none is a Bearer challenge, the first
// parsed challenge is returned. An empty or unparseable header yields a
// zero-value Challenge.
func ParseChallenge(header string) Challenge {
	challenges := splitChallenges(header)
	if len(challenges) == 0 {
		return Challenge{}
	}
	for _, c := range challenges {
		if strings.EqualFold(c.Scheme, "Bearer") {
			return c
		}
	}
	return challenges[0]
}

// ParseChallenges joins multiple WWW-Authenticate header lines (as returned
// by http.Header.Values) and parses them with ParseChallenge. It is safe to
// call with an empty slice (returns a zero-value Challenge).
func ParseChallenges(headers []string) Challenge {
	if len(headers) == 0 {
		return Challenge{}
	}
	return ParseChallenge(strings.Join(headers, ", "))
}

// splitChallenges tokenizes a WWW-Authenticate header value into its
// constituent challenges. RFC 7235's grammar for combining multiple
// challenges in one header is a well-known ambiguity (a bare auth-param
// value and a following scheme token are not syntactically distinguishable
// in general), so this uses the same practical heuristic most HTTP client
// libraries use: split on commas that are not inside a quoted string, then
// treat any resulting segment whose first whitespace-delimited word
// contains no '=' as the start of a new challenge (that word is the
// scheme), and every other segment as an auth-param of the current
// challenge.
func splitChallenges(header string) []Challenge {
	var challenges []Challenge
	var current *Challenge

	for _, segment := range splitTopLevelCommas(header) {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		firstSpace := strings.IndexAny(segment, " \t")
		if firstSpace == -1 {
			if !strings.Contains(segment, "=") {
				// Bare scheme token (e.g. token68 form, or a challenge with
				// no auth-params at all).
				if current != nil {
					challenges = append(challenges, *current)
				}
				current = &Challenge{Scheme: segment}
				continue
			}
			// A lone "key=value" with no scheme word before it: only
			// possible as a continuation of the current challenge.
			applyParam(current, segment)
			continue
		}

		word1 := segment[:firstSpace]
		rest := strings.TrimSpace(segment[firstSpace:])
		if strings.Contains(word1, "=") {
			// word1 itself looks like a param, not a scheme: this whole
			// segment continues the current challenge.
			applyParam(current, segment)
			continue
		}

		// word1 is a bare token: this segment starts a new challenge, and
		// rest (if any) is that challenge's first auth-param.
		if current != nil {
			challenges = append(challenges, *current)
		}
		current = &Challenge{Scheme: word1}
		if rest != "" {
			applyParam(current, rest)
		}
	}
	if current != nil {
		challenges = append(challenges, *current)
	}
	return challenges
}

// applyParam parses one "key=value" (optionally double-quoted value) auth-
// param and merges it into c's known fields. Unrecognized keys (e.g.
// "realm") are silently ignored — Challenge only models the fields the MCP
// step-up flow needs. It is a no-op when c is nil (a malformed header whose
// first segment was itself a bare param, with no scheme).
func applyParam(c *Challenge, param string) {
	if c == nil {
		return
	}
	eq := strings.IndexByte(param, '=')
	if eq < 0 {
		return
	}
	key := strings.TrimSpace(param[:eq])
	value := strings.TrimSpace(param[eq+1:])
	value = unquote(value)

	switch strings.ToLower(key) {
	case "error":
		c.Error = value
	case "error_description":
		c.ErrorDescription = value
	case "resource_metadata":
		c.ResourceMetadata = value
	case "scope":
		if value == "" {
			c.Scope = nil
			return
		}
		c.Scope = strings.Fields(value)
	}
}

// unquote strips a single matching pair of surrounding double quotes and
// unescapes \" and \\, per RFC 7230 §3.2.6's quoted-string grammar. Values
// that aren't quoted are returned unchanged.
func unquote(v string) string {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return v
	}
	inner := v[1 : len(v)-1]
	var b strings.Builder
	b.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// splitTopLevelCommas splits s on commas that are not inside a double-quoted
// substring, so a comma inside a quoted auth-param value (e.g.
// error_description="access denied, try again") does not get mistaken for a
// challenge/param separator.
func splitTopLevelCommas(s string) []string {
	var out []string
	var b strings.Builder
	inQuotes := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case escaped:
			b.WriteByte(ch)
			escaped = false
		case ch == '\\' && inQuotes:
			b.WriteByte(ch)
			escaped = true
		case ch == '"':
			inQuotes = !inQuotes
			b.WriteByte(ch)
		case ch == ',' && !inQuotes:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(ch)
		}
	}
	out = append(out, b.String())
	return out
}
