package redact

import "unicode/utf8"

const truncatedSuffix = "…[truncated]"

// Truncate cuts s to at most max bytes, taking care not to split a
// multi-byte rune, and appends a "…[truncated]" marker when it actually cut
// something. max <= 0 always returns "".
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	b := []byte(s)[:max]
	// Trim back over any rune left dangling mid-sequence by the byte cut.
	for len(b) > 0 {
		r, size := utf8.DecodeLastRune(b)
		if r != utf8.RuneError || size != 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return string(b) + truncatedSuffix
}
