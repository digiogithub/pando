package util

import "testing"

func TestCopyToClipboardRejectsEmptyInput(t *testing.T) {
	if CopyToClipboard("") {
		t.Fatal("CopyToClipboard(\"\") = true, want false")
	}
	if CopyToClipboard("   ") {
		t.Fatal("CopyToClipboard(whitespace-only) = true, want false")
	}
}

func TestCopyToClipboardIsInjectable(t *testing.T) {
	prev := CopyToClipboard
	t.Cleanup(func() { CopyToClipboard = prev })

	var got string
	CopyToClipboard = func(text string) bool {
		got = text
		return true
	}

	if !CopyToClipboard("hello") {
		t.Fatal("fake CopyToClipboard returned false")
	}
	if got != "hello" {
		t.Fatalf("fake received %q, want %q", got, "hello")
	}
}
