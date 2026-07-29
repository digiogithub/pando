package tools

import (
	"strings"
	"testing"
)

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"ascii", []byte("# Title\n\nplain markdown\n"), false},
		{"null byte", []byte("text\x00more"), true},
		{"elf header", []byte("\x7fELF\x02\x01\x01\x00\x00\x00"), true},
		{
			// Regression: UTF-8 punctuation pushed the old ASCII-only byte scan
			// past the 30% threshold, so ordinary Markdown was rejected as binary.
			name: "utf8 punctuation",
			data: []byte(strings.Repeat("# AGENTS.md — AI Costs — Platform “quoted” · ok\n", 40)),
			want: false,
		},
		{
			name: "cjk text",
			data: []byte(strings.Repeat("本枝葉林森 — 説明文です\n", 40)),
			want: false,
		},
		{
			name: "emoji heavy",
			data: []byte(strings.Repeat("status: ✅ done 🚀 shipped\n", 40)),
			want: false,
		},
		{
			// A fixed-size sample can cut a multi-byte rune in half; the trailing
			// fragment must not count as garbage.
			name: "truncated trailing rune",
			data: append([]byte(strings.Repeat("hello world\n", 20)), 0xE2, 0x80),
			want: false,
		},
		{
			name: "random high bytes",
			data: []byte(strings.Repeat("\xff\xfe\xfd\xfc", 100)),
			want: true,
		},
		{
			name: "control characters",
			data: []byte(strings.Repeat("\x01\x02\x03\x04", 100)),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinaryContent(tt.data); got != tt.want {
				t.Fatalf("isBinaryContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
