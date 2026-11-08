package kb

import (
	"strings"
	"testing"
)

func TestParseDocumentStripsFrontMatterAndKeepsLines(t *testing.T) {
	content := "---\ntitle: Login spec\nlabels: [auth, security]\naliases: [signin]\nstatus: proposed\n---\n# Heading\n\nBody text.\n"
	p := parseDocument(content)
	if strings.Contains(p.Body, "title:") {
		t.Fatalf("front matter left in body: %q", p.Body)
	}
	if p.BodyLine != 7 {
		t.Fatalf("BodyLine = %d, want 7", p.BodyLine)
	}
	if p.Fields != (docFields{Title: "Login spec", Labels: "auth security", Aliases: "signin"}) {
		t.Fatalf("fields = %+v", p.Fields)
	}
	chunks := chunkLines(p.Body, p.BodyLine, 1000, 0)
	if len(chunks) != 1 || chunks[0].StartLine != 7 || chunks[0].EndLine != 9 {
		t.Fatalf("chunks = %+v", chunks)
	}
	lines := strings.Split(content, "\n")
	if lines[chunks[0].StartLine-1] != "# Heading" || lines[chunks[0].EndLine-1] != "Body text." {
		t.Fatalf("line range does not point at the source: %+v", chunks[0])
	}
}

func TestParseDocumentTitleFallsBackToHeading(t *testing.T) {
	p := parseDocument("intro\n# Real title\ntext")
	if p.Fields.Title != "Real title" || p.BodyLine != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestChunkLinesKeepsSpecBlocksWhole(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Requirements\n\n")
	b.WriteString("### AUTH.R1 Throttle logins\n\n")
	for i := 0; i < 20; i++ {
		b.WriteString("The system shall throttle repeated failed logins per account.\n\n")
	}
	b.WriteString("```\n### NOT.R9 inside a fence\n```\n")
	b.WriteString("### AUTH.R2 Lockout\n\nShort.\n")
	body := b.String()
	chunks := chunkLines(body, 1, 200, 20)

	var r1 *lineChunk
	for i := range chunks {
		if strings.HasPrefix(chunks[i].Text, "### AUTH.R1") {
			r1 = &chunks[i]
		}
		if strings.Contains(chunks[i].Text, "throttle") && !strings.HasPrefix(chunks[i].Text, "### AUTH.R1") {
			t.Fatalf("AUTH.R1 was split: %+v", chunks[i])
		}
	}
	if r1 == nil || len(r1.Text) <= 200 {
		t.Fatalf("AUTH.R1 should be one oversized chunk, got %+v", chunks)
	}
	if !strings.Contains(r1.Text, "NOT.R9") {
		t.Fatalf("a fenced heading must not end the requirement block: %q", r1.Text)
	}
	last := chunks[len(chunks)-1]
	if !strings.HasPrefix(last.Text, "### AUTH.R2") || last.EndLine != strings.Count(body, "\n") {
		t.Fatalf("last chunk = %+v", last)
	}
}

func TestChunkLinesSplitsLongParagraphsOnLines(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "line of prose number " + strings.Repeat("x", 10)
	}
	chunks := chunkLines(strings.Join(lines, "\n"), 5, 200, 0)
	if len(chunks) < 3 {
		t.Fatalf("want several chunks, got %d", len(chunks))
	}
	next := 5
	for _, c := range chunks {
		if len(c.Text) > 200 || c.StartLine != next {
			t.Fatalf("chunk %+v (want start %d, <= 200 bytes)", c, next)
		}
		next = c.EndLine + 1
	}
	if next != 35 {
		t.Fatalf("chunks end at line %d, want 34", next-1)
	}
}

func TestChunkLinesCutsHugeLines(t *testing.T) {
	chunks := chunkLines("short\n\n"+strings.Repeat("word ", 200), 1, 100, 10)
	if len(chunks) < 3 || chunks[0].Text != "short" {
		t.Fatalf("chunks = %+v", chunks)
	}
	for _, c := range chunks[1:] {
		if c.StartLine != 3 || c.EndLine != 3 || len(c.Text) > 100 {
			t.Fatalf("huge-line piece %+v", c)
		}
	}
}
