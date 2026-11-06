package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderMessageHasNoBox(t *testing.T) {
	for _, plain := range []bool{false, true} {
		SetPlainMode(plain)
		out := ansi.Strip(renderMessage("hello world", true, false, 40))
		for _, r := range "┃│┏┗╻┌└─┐┘╭╰" {
			if strings.ContainsRune(out, r) {
				t.Fatalf("plain=%v: box rune %q in %q", plain, r, out)
			}
		}
		if !strings.HasPrefix(out, "> ") {
			t.Fatalf("plain=%v: user message must start with '> ', got %q", plain, out)
		}
	}
	SetPlainMode(false)
}

func TestPlainModeIsASCIIAndUncolored(t *testing.T) {
	SetPlainMode(true)
	defer SetPlainMode(false)
	out := renderMessage("answer", false, false, 40)
	if out != ansi.Strip(out) {
		t.Fatalf("plain output contains ANSI: %q", out)
	}
	if !strings.HasPrefix(out, "* ") {
		t.Fatalf("assistant marker want '* ', got %q", out)
	}
	tool := roleBlock("line1\nline2", roleTool)
	if tool != "- line1\n  line2" {
		t.Fatalf("tool block = %q", tool)
	}
}

func TestNoticeMsgAndClear(t *testing.T) {
	m := &messagesCmp{cachedContent: map[string]cacheItem{}}
	m.width = 60
	m.Update(NoticeMsg{Text: "copilot\n  gpt-4o"})
	if len(m.notices) != 1 {
		t.Fatalf("notice not stored")
	}
	m.Update(SessionClearedMsg{})
	if len(m.notices) != 0 {
		t.Fatalf("notices must be cleared with the session")
	}
}
