package telemetry

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// NewDebugID generates a new anonymous debug identifier: 16 random decimal
// digits, numeric so it is easy to dictate and paste into a support issue.
// The first digit is never zero, so the id never displays with a misleading
// leading zero. Each digit is drawn with crypto/rand.Int, which is unbiased
// by construction (it rejection-samples internally), so no digit is more
// likely than any other.
func NewDebugID() (string, error) {
	var b strings.Builder
	b.Grow(16)

	first, err := randDebugDigit(1, 9)
	if err != nil {
		return "", err
	}
	b.WriteByte(first)

	for i := 1; i < 16; i++ {
		digit, err := randDebugDigit(0, 9)
		if err != nil {
			return "", err
		}
		b.WriteByte(digit)
	}
	return b.String(), nil
}

// randDebugDigit returns a uniformly random ASCII digit byte in [min, max].
func randDebugDigit(min, max int) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, fmt.Errorf("generate random debug id digit: %w", err)
	}
	return byte('0' + min + int(n.Int64())), nil
}

// ValidDebugID reports whether id is exactly 16 decimal digits.
func ValidDebugID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FormatDebugID renders a stored debug id (16 digits, no separators) grouped
// for display and copy-to-clipboard, e.g. "1234-5678-9012-3456". A value that
// is not exactly 16 digits — empty (never generated) or otherwise malformed —
// is returned unchanged rather than rendered as a confusing partial group.
func FormatDebugID(id string) string {
	if !ValidDebugID(id) {
		return id
	}
	return id[0:4] + "-" + id[4:8] + "-" + id[8:12] + "-" + id[12:16]
}
