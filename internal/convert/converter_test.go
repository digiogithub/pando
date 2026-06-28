package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertFileXLSX(t *testing.T) {
	c := New()
	md, err := c.ConvertFile(filepath.Join("testdata", "sample.xlsx"))
	if err != nil {
		t.Fatalf("ConvertFile(xlsx) error: %v", err)
	}
	if !strings.Contains(md, "Pando") || !strings.Contains(md, "42") {
		t.Fatalf("converted markdown missing expected cells, got:\n%s", md)
	}
}

func TestConvertFileCSV(t *testing.T) {
	c := New()
	md, err := c.ConvertFile(filepath.Join("testdata", "sample.csv"))
	if err != nil {
		t.Fatalf("ConvertFile(csv) error: %v", err)
	}
	if !strings.Contains(md, "Pando") {
		t.Fatalf("converted markdown missing expected content, got:\n%s", md)
	}
}

func TestConvertReader(t *testing.T) {
	c := New()
	f, err := os.Open(filepath.Join("testdata", "sample.csv"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	md, err := c.ConvertReader(f, "sample.csv")
	if err != nil {
		t.Fatalf("ConvertReader error: %v", err)
	}
	if !strings.Contains(md, "Pando") {
		t.Fatalf("reader conversion missing content, got:\n%s", md)
	}
}

func TestConvertFileUnsupported(t *testing.T) {
	c := New()
	if _, err := c.ConvertFile("notes.go"); err == nil {
		t.Fatal("expected error for unsupported extension, got nil")
	}
}

func TestIsSupported(t *testing.T) {
	cases := map[string]bool{
		"a.docx":   true,
		"a.PDF":    true,
		"a.xlsx":   true,
		"a.txt":    true,
		"a.go":     false,
		"noext":    false,
		"dir/x.md": true,
	}
	for path, want := range cases {
		if got := IsSupported(path); got != want {
			t.Errorf("IsSupported(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestIsConvertibleDocument(t *testing.T) {
	c := New()
	cases := map[string]bool{
		"report.docx": true,
		"sheet.xlsx":  true,
		"book.epub":   true,
		"page.html":   true,
		"notes.md":    false, // markdown is indexed verbatim, not converted
		"data.json":   false, // excluded from curated KB set
		"plain.txt":   false,
		"code.go":     false,
	}
	for path, want := range cases {
		if got := c.IsConvertibleDocument(path); got != want {
			t.Errorf("IsConvertibleDocument(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSupportedExtensionsSorted(t *testing.T) {
	exts := SupportedExtensions()
	if len(exts) == 0 {
		t.Fatal("expected non-empty supported extensions")
	}
	for i := 1; i < len(exts); i++ {
		if exts[i-1] > exts[i] {
			t.Fatalf("extensions not sorted at %d: %q > %q", i, exts[i-1], exts[i])
		}
	}
}
