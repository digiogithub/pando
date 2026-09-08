package telemetry

import (
	"strings"
	"testing"
)

func TestNewDebugIDFormat(t *testing.T) {
	id, err := NewDebugID()
	if err != nil {
		t.Fatalf("NewDebugID() error = %v", err)
	}
	if len(id) != 16 {
		t.Fatalf("NewDebugID() length = %d, want 16 (%q)", len(id), id)
	}
	if id[0] == '0' {
		t.Fatalf("NewDebugID() first digit is 0: %q", id)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			t.Fatalf("NewDebugID() contains non-digit %q in %q", r, id)
		}
	}
	if !ValidDebugID(id) {
		t.Fatalf("ValidDebugID(%q) = false, want true", id)
	}
}

func TestNewDebugIDUniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id, err := NewDebugID()
		if err != nil {
			t.Fatalf("NewDebugID() error = %v", err)
		}
		if seen[id] {
			t.Fatalf("NewDebugID() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestNewDebugIDFirstDigitNeverZero(t *testing.T) {
	for i := 0; i < 500; i++ {
		id, err := NewDebugID()
		if err != nil {
			t.Fatalf("NewDebugID() error = %v", err)
		}
		if strings.HasPrefix(id, "0") {
			t.Fatalf("NewDebugID() = %q, first digit must not be 0", id)
		}
	}
}

func TestFormatDebugID(t *testing.T) {
	got := FormatDebugID("1234567890123456")
	want := "1234-5678-9012-3456"
	if got != want {
		t.Fatalf("FormatDebugID() = %q, want %q", got, want)
	}
}

func TestFormatDebugIDPassthroughWhenInvalid(t *testing.T) {
	for _, id := range []string{"", "short", "12345678901234567", "abcd567890123456"} {
		if got := FormatDebugID(id); got != id {
			t.Fatalf("FormatDebugID(%q) = %q, want unchanged", id, got)
		}
	}
}

func TestValidDebugID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"1234567890123456", true},
		{"", false},
		{"123456789012345", false},
		{"12345678901234567", false},
		{"123456789012345a", false},
	}
	for _, c := range cases {
		if got := ValidDebugID(c.id); got != c.want {
			t.Fatalf("ValidDebugID(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}
