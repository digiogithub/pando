package provider

import (
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

func TestSanitizeToolCallArguments(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"valid object":    {`{"file_path":"a.go"}`, `{"file_path":"a.go"}`},
		"empty":           {"", "{}"},
		"bare string":     {`"hello"`, "{}"},
		"array":           {`[1,2]`, "{}"},
		"null":            {"null", "{}"},
		"garbage":         {"not json at all {{{", "{}"},
		"truncated write": {`{"file_path":"a.go","content":"package main\nfunc`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeToolCallArguments("write", tc.in)
			if m := sanitizeToolCallArgumentsMap("write", tc.in); m == nil {
				t.Fatalf("map must never be nil")
			}
			if tc.want == "" {
				// Repaired or replaced, it must decode as an object.
				if sanitizeToolCallArgumentsMap("write", tc.in) == nil {
					t.Fatalf("expected object, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDropTruncatedToolCalls(t *testing.T) {
	calls := []message.ToolCall{
		{ID: "1", Name: "view", Input: `{"file_path":"a"}`},
		{ID: "2", Name: "write", Input: `{"file_path":"b","content":"trunc`},
		{ID: "3", Name: "ls", Input: ""},
	}
	got := dropTruncatedToolCalls(calls)
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "3" {
		t.Fatalf("unexpected result: %+v", got)
	}
}
