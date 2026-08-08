package app

import "testing"

func TestNormalizeEnrichedBlock(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		maxChars int
		want     string
	}{
		{
			name: "extracts tagged block",
			raw:  "here you go\n<enriched_context>\n## Code\n- a.go:1 Foo — does x\n</enriched_context>\ntrailing",
			want: "<enriched_context>\n## Code\n- a.go:1 Foo — does x\n</enriched_context>",
		},
		{
			name: "wraps untagged output",
			raw:  "## Code\n- a.go:1 Foo",
			want: "<enriched_context>\n## Code\n- a.go:1 Foo\n</enriched_context>",
		},
		{name: "no relevant context", raw: "NO_RELEVANT_CONTEXT", want: ""},
		{name: "empty", raw: "   ", want: ""},
		{
			name:     "truncates to max chars",
			raw:      "<enriched_context>\nabcdefghij\n</enriched_context>",
			maxChars: 4,
			want:     "<enriched_context>\nabcd\n… (truncated)\n</enriched_context>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeEnrichedBlock(tt.raw, tt.maxChars)
			if got != tt.want {
				t.Errorf("normalizeEnrichedBlock() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
