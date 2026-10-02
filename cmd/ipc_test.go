package cmd

import "testing"

func TestFormatInstanceWebPort(t *testing.T) {
	if got := formatInstanceWebPort(4123); got != "web :4123" {
		t.Fatalf("formatInstanceWebPort(4123) = %q", got)
	}
	if got := formatInstanceWebPort(0); got != "" {
		t.Fatalf("formatInstanceWebPort(0) = %q", got)
	}
}

func TestFormatInstanceParent(t *testing.T) {
	if got := formatInstanceParent("12345678-parent"); got != "child 12345678" {
		t.Fatalf("formatInstanceParent() = %q", got)
	}
	if got := formatInstanceParent(""); got != "" {
		t.Fatalf("formatInstanceParent(empty) = %q", got)
	}
}
