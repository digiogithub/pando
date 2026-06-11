package tools

import (
	"testing"
)

func TestResolveToolAlias(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"read", "view"},
		{"str_replace_editor", "edit"},
		{"create_file", "write"},
		{"run_command", "bash"},
		{"find_files", "glob"},
		{"search_files", "grep"},
		{"list_directory", "ls"},
		// Already canonical names should be returned unchanged.
		{"view", "view"},
		{"edit", "edit"},
		{"bash", "bash"},
		// Unknown names should be returned unchanged.
		{"some_unknown_tool", "some_unknown_tool"},
	}
	for _, tc := range cases {
		got := ResolveToolAlias(tc.input)
		if got != tc.expected {
			t.Errorf("ResolveToolAlias(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestCrossModelAliasNames(t *testing.T) {
	names := CrossModelAliasNames()
	if len(names) == 0 {
		t.Fatal("CrossModelAliasNames returned empty slice")
	}
	// Verify "read" is in the list
	found := false
	for _, n := range names {
		if n == "read" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CrossModelAliasNames does not contain 'read'")
	}
}
