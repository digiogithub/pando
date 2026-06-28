// Package convert wraps the conductor-oss/markitdown pure-Go library to turn
// rich document formats (docx, pdf, xlsx, …) into Markdown. It is the single
// integration point for document conversion across Pando: the `pando convert`
// CLI command and the on-the-fly Knowledge Base indexing both go through it.
package convert

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	markitdown "github.com/conductor-oss/markitdown"
)

// supportedExtensions is the full set of input extensions the underlying
// library can convert to Markdown.
var supportedExtensions = map[string]struct{}{
	".pdf":      {},
	".docx":     {},
	".pptx":     {},
	".xlsx":     {},
	".xls":      {},
	".html":     {},
	".htm":      {},
	".csv":      {},
	".epub":     {},
	".ipynb":    {},
	".xml":      {},
	".rss":      {},
	".atom":     {},
	".json":     {},
	".jsonl":    {},
	".txt":      {},
	".md":       {},
	".markdown": {},
	".zip":      {},
}

// convertibleDocumentExtensions is the curated subset used for automatic
// Knowledge Base ingestion. It deliberately excludes plain-text/markdown and
// generic data formats (.txt/.md/.markdown/.json/.jsonl/.xml) which are either
// already indexed verbatim or too ambiguous to convert without surprising the
// user. The KB layer can override this set via configuration.
var convertibleDocumentExtensions = map[string]struct{}{
	".pdf":   {},
	".docx":  {},
	".pptx":  {},
	".xlsx":  {},
	".xls":   {},
	".epub":  {},
	".ipynb": {},
	".csv":   {},
	".html":  {},
	".htm":   {},
	".rss":   {},
	".atom":  {},
}

// Converter wraps a markitdown engine. The engine is created lazily on first
// use because initializing the PDFium WASM runtime has a non-trivial cost; a
// process that never converts a PDF should not pay for it. All conversions are
// serialized with a mutex: the underlying PDF backend is not guaranteed to be
// safe for concurrent use, and KB conversion is not a hot path.
type Converter struct {
	mu     sync.Mutex
	engine *markitdown.MarkItDown
	// convertible is the set used by IsConvertibleDocument. When nil the
	// package default (convertibleDocumentExtensions) applies.
	convertible map[string]struct{}
}

// New returns a ready-to-use Converter using the default curated document set
// for KB ingestion. The heavy engine is initialized lazily.
func New() *Converter {
	return &Converter{}
}

// NewWithConvertibleExtensions returns a Converter whose curated KB set is
// overridden by exts (each entry with or without a leading dot, case
// insensitive). An empty list falls back to the built-in default set.
func NewWithConvertibleExtensions(exts []string) *Converter {
	c := &Converter{}
	if len(exts) == 0 {
		return c
	}
	set := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		set[e] = struct{}{}
	}
	if len(set) > 0 {
		c.convertible = set
	}
	return c
}

func (c *Converter) getEngine() *markitdown.MarkItDown {
	if c.engine == nil {
		c.engine = markitdown.New()
	}
	return c.engine
}

// ConvertFile converts a local file at path to Markdown and returns the
// resulting text. The path's extension must be supported.
func (c *Converter) ConvertFile(path string) (string, error) {
	if !IsSupported(path) {
		return "", fmt.Errorf("convert: unsupported file format %q", filepath.Ext(path))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.getEngine().ConvertFile(path)
	if err != nil {
		return "", fmt.Errorf("convert: %s: %w", filepath.Base(path), err)
	}
	if res == nil {
		return "", nil
	}
	return res.Markdown, nil
}

// ConvertReader converts the contents of r to Markdown. name is used to detect
// the format via its extension (e.g. "report.docx").
func (c *Converter) ConvertReader(r io.ReadSeeker, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info := markitdown.StreamInfo{
		Extension: strings.ToLower(filepath.Ext(name)),
		Filename:  filepath.Base(name),
	}
	res, err := c.getEngine().ConvertReader(r, info)
	if err != nil {
		return "", fmt.Errorf("convert: %s: %w", filepath.Base(name), err)
	}
	if res == nil {
		return "", nil
	}
	return res.Markdown, nil
}

// Convert auto-detects whether source is a local file path or a URL and
// converts it to Markdown.
func (c *Converter) Convert(source string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.getEngine().Convert(source)
	if err != nil {
		return "", fmt.Errorf("convert: %s: %w", source, err)
	}
	if res == nil {
		return "", nil
	}
	return res.Markdown, nil
}

// IsSupported reports whether path's extension is convertible by the library.
func IsSupported(path string) bool {
	_, ok := supportedExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IsConvertibleDocument reports whether path belongs to the curated set of
// document formats that should be auto-converted during KB ingestion.
func (c *Converter) IsConvertibleDocument(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	set := c.convertible
	if set == nil {
		set = convertibleDocumentExtensions
	}
	_, ok := set[ext]
	return ok
}

// SupportedExtensions returns the sorted list of all supported input
// extensions (including the leading dot), suitable for display.
func SupportedExtensions() []string {
	return sortedKeys(supportedExtensions)
}

// ConvertibleDocumentExtensions returns the sorted list of extensions in the
// curated KB ingestion set.
func ConvertibleDocumentExtensions() []string {
	return sortedKeys(convertibleDocumentExtensions)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// simple insertion sort to avoid pulling in sort for a tiny slice churn;
	// keep it deterministic for stable CLI output.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
