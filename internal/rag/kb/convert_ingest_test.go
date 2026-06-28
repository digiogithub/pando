package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/convert"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestIsIndexableFile(t *testing.T) {
	conv := convert.New()
	cases := []struct {
		path string
		conv DocumentConverter
		want bool
	}{
		{"notes.md", nil, true},
		{"notes.md", conv, true},
		{"report.docx", nil, false}, // no converter → only markdown
		{"report.docx", conv, true},
		{"data.csv", conv, true},
		{"data.json", conv, false}, // excluded from curated set
		{"code.go", conv, false},
	}
	for _, tc := range cases {
		if got := isIndexableFile(tc.path, tc.conv); got != tc.want {
			t.Errorf("isIndexableFile(%q, conv=%v) = %v, want %v", tc.path, tc.conv != nil, got, tc.want)
		}
	}
}

func TestLoadDocumentBodyMarkdownVerbatim(t *testing.T) {
	dir := t.TempDir()
	md := "# Title\n\nbody text\n"
	p := writeFile(t, dir, "a.md", md)

	content, format, converted, err := loadDocumentBody(p, convert.New())
	if err != nil {
		t.Fatalf("loadDocumentBody: %v", err)
	}
	if converted {
		t.Error("markdown should not be marked converted")
	}
	if format != "md" {
		t.Errorf("format = %q, want md", format)
	}
	if content != md {
		t.Errorf("markdown content altered: %q", content)
	}
}

func TestLoadDocumentBodyConvertsDocument(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "b.csv", "Name,Score\nPando,42\n")

	content, format, converted, err := loadDocumentBody(p, convert.New())
	if err != nil {
		t.Fatalf("loadDocumentBody: %v", err)
	}
	if !converted {
		t.Error("csv document should be marked converted")
	}
	if format != "csv" {
		t.Errorf("format = %q, want csv", format)
	}
	if !strings.Contains(content, "Pando") || !strings.Contains(content, "|") {
		t.Errorf("expected markdown table content, got:\n%s", content)
	}
}

func TestLoadDocumentBodyNoConverterReadsRaw(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "c.csv", "a,b\n1,2\n")

	// With no converter, a .csv is read verbatim (and would not be indexed
	// because isIndexableFile would have filtered it out upstream).
	content, _, converted, err := loadDocumentBody(p, nil)
	if err != nil {
		t.Fatalf("loadDocumentBody: %v", err)
	}
	if converted {
		t.Error("without converter nothing should be converted")
	}
	if !strings.Contains(content, "a,b") {
		t.Errorf("expected raw csv, got: %q", content)
	}
}
