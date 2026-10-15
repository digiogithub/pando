package convert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/logging"
)

// Default limits of the conversion endpoint.
const (
	DefaultMaxInputBytes  = 20 << 20 // 20 MiB
	DefaultMaxOutputBytes = 8 << 20  // 8 MiB
	DefaultTimeout        = 60 * time.Second
	DefaultMaxConcurrent  = 2
)

// mediaTypeExt maps the media types the endpoint accepts to the canonical
// extension the converter dispatches on.
var mediaTypeExt = map[string]string{
	"application/pdf": ".pdf",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.ms-excel": ".xls",
	"text/html":                ".html",
	"application/xhtml+xml":    ".html",
	"text/csv":                 ".csv",
	"application/epub+zip":     ".epub",
	"application/x-ipynb+json": ".ipynb",
	"application/xml":          ".xml",
	"text/xml":                 ".xml",
	"application/rss+xml":      ".rss",
	"application/atom+xml":     ".atom",
	"application/json":         ".json",
	"application/x-ndjson":     ".jsonl",
	"application/jsonl":        ".jsonl",
	"text/plain":               ".txt",
	"text/markdown":            ".md",
}

// httpExcludedExtensions are supported by the library but refused over HTTP:
// a .zip is converted entry by entry, recursively, which turns an untrusted
// upload into an archive bomb.
var httpExcludedExtensions = map[string]bool{".zip": true}

// HTTPOptions configures the conversion endpoint. Zero values take the
// package defaults.
type HTTPOptions struct {
	// MaxInputBytes caps the uploaded document (413 above it).
	MaxInputBytes int64
	// MaxOutputBytes caps the Markdown produced (422 above it).
	MaxOutputBytes int64
	// Timeout bounds one conversion, including the wait for a slot (504).
	Timeout time.Duration
	// MaxConcurrent is how many conversions run at once; others wait for a
	// slot within Timeout.
	MaxConcurrent int
	// Runner performs the conversion; nil means a ProcessRunner.
	Runner Runner
}

// ConvertResponse is the JSON answer of the endpoint.
type ConvertResponse struct {
	Markdown string          `json:"markdown"`
	Metadata ConvertMetadata `json:"metadata"`
}

// ConvertMetadata describes one conversion.
type ConvertMetadata struct {
	// Filename is the client's file name hint, sanitised; may be empty.
	Filename string `json:"filename,omitempty"`
	// Format is the detected format, the extension without the dot.
	Format string `json:"format"`
	// MediaType is the media type the format was resolved from, if any.
	MediaType string `json:"media_type,omitempty"`
	// InputBytes is the size of the document.
	InputBytes int `json:"input_bytes"`
	// MarkdownBytes is the size of the Markdown.
	MarkdownBytes int `json:"markdown_bytes"`
	// DurationMs is the conversion time in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

type httpHandler struct {
	o     HTTPOptions
	slots chan struct{}
}

// NewHTTPHandler returns the document conversion endpoint. It accepts
// POST with either the raw document as the body (its media type in
// Content-Type, an optional file name in X-Filename) or a multipart form with
// the document in the "file" field. It answers the Markdown as text/markdown,
// or as JSON with metadata when the client accepts application/json and not
// text/markdown. Authentication is the caller's: mount it behind the
// server's auth middleware.
func NewHTTPHandler(o HTTPOptions) http.Handler {
	if o.MaxInputBytes <= 0 {
		o.MaxInputBytes = DefaultMaxInputBytes
	}
	if o.MaxOutputBytes <= 0 {
		o.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = DefaultMaxConcurrent
	}
	if o.Runner == nil {
		o.Runner = ProcessRunner{}
	}
	return &httpHandler{o: o, slots: make(chan struct{}, o.MaxConcurrent)}
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeConvertError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if r.ContentLength > h.o.MaxInputBytes {
		writeConvertError(w, http.StatusRequestEntityTooLarge, "too_large", "document exceeds "+strconv.FormatInt(h.o.MaxInputBytes, 10)+" bytes")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.o.MaxInputBytes)

	data, mediaType, filename, err := h.readDocument(r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeConvertError(w, http.StatusRequestEntityTooLarge, "too_large", "document exceeds "+strconv.FormatInt(h.o.MaxInputBytes, 10)+" bytes")
			return
		}
		writeConvertError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(data) == 0 {
		writeConvertError(w, http.StatusBadRequest, "empty_document", "the document is empty")
		return
	}
	ext := resolveFormat(mediaType, filename)
	if ext == "" {
		writeConvertError(w, http.StatusUnsupportedMediaType, "unsupported_format",
			"unsupported document format; supported: "+strings.Join(HTTPSupportedExtensions(), " "))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.o.Timeout)
	defer cancel()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		w.Header().Set("Retry-After", "5")
		writeConvertError(w, http.StatusServiceUnavailable, "busy", "no conversion slot became free in time")
		return
	}

	start := time.Now()
	md, err := h.o.Runner.Run(ctx, ext, data, h.o.MaxOutputBytes)
	elapsed := time.Since(start)
	if err != nil {
		h.writeRunError(w, err, ext, len(data), elapsed)
		return
	}
	md = strings.ToValidUTF8(md, "�")
	meta := ConvertMetadata{
		Filename:      filename,
		Format:        strings.TrimPrefix(ext, "."),
		MediaType:     mediaType,
		InputBytes:    len(data),
		MarkdownBytes: len(md),
		DurationMs:    elapsed.Milliseconds(),
	}
	logging.Debug("convert: document converted", "format", meta.Format, "input_bytes", meta.InputBytes,
		"markdown_bytes", meta.MarkdownBytes, "duration_ms", meta.DurationMs)

	w.Header().Set("X-Pando-Convert-Format", meta.Format)
	w.Header().Set("X-Pando-Convert-Input-Bytes", strconv.Itoa(meta.InputBytes))
	w.Header().Set("X-Pando-Convert-Duration-Ms", strconv.FormatInt(meta.DurationMs, 10))
	w.Header().Set("Cache-Control", "no-store")
	if wantsJSON(r.Header.Get("Accept")) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ConvertResponse{Markdown: md, Metadata: meta})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, md)
}

// readDocument returns the document bytes, its declared media type and the
// sanitised file name, from a raw body or a multipart "file" field.
func (h *httpHandler) readDocument(r *http.Request) ([]byte, string, string, error) {
	mediaType := parseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		data, err := io.ReadAll(r.Body)
		return data, mediaType, sanitizeFilename(r.Header.Get("X-Filename")), err
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", "", errors.New("malformed multipart body")
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, "", "", errors.New(`multipart body has no "file" field`)
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, "", "", err
			}
			return nil, "", "", errors.New("malformed multipart body")
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		return data, parseMediaType(part.Header.Get("Content-Type")), sanitizeFilename(part.FileName()), err
	}
}

func (h *httpHandler) writeRunError(w http.ResponseWriter, err error, ext string, size int, elapsed time.Duration) {
	format := strings.TrimPrefix(ext, ".")
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		logging.Warn("convert: conversion timed out", "format", format, "input_bytes", size, "timeout", h.o.Timeout)
		writeConvertError(w, http.StatusGatewayTimeout, "timeout", "conversion exceeded "+h.o.Timeout.String())
	case errors.Is(err, context.Canceled):
		// Client went away; nothing useful to answer.
		writeConvertError(w, http.StatusServiceUnavailable, "canceled", "request canceled")
	case errors.Is(err, ErrOutputTooLarge):
		writeConvertError(w, http.StatusUnprocessableEntity, "output_too_large",
			"markdown exceeds "+strconv.FormatInt(h.o.MaxOutputBytes, 10)+" bytes")
	case errors.Is(err, ErrConversionFailed), errors.Is(err, ErrUnsupportedFormat):
		// The detail can quote the document; log it, do not echo it.
		logging.Debug("convert: conversion failed", "format", format, "input_bytes", size, "duration", elapsed, "error", err)
		writeConvertError(w, http.StatusUnprocessableEntity, "conversion_failed", "the document could not be converted")
	default:
		logging.Error("convert: converter unavailable", "format", format, "error", err)
		writeConvertError(w, http.StatusInternalServerError, "converter_error", "converter unavailable")
	}
}

// resolveFormat picks the canonical extension: from the media type when it
// is a known one, else (no type, or application/octet-stream) from the file
// name's extension. Any other media type is unsupported.
func resolveFormat(mediaType, filename string) string {
	if ext, ok := mediaTypeExt[mediaType]; ok {
		return ext
	}
	if mediaType != "" && mediaType != "application/octet-stream" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".htm" {
		ext = ".html"
	}
	if ext == ".markdown" {
		ext = ".md"
	}
	if httpExcludedExtensions[ext] {
		return ""
	}
	if _, ok := supportedExtensions[ext]; !ok {
		return ""
	}
	return ext
}

// HTTPSupportedExtensions lists the extensions the endpoint converts.
func HTTPSupportedExtensions() []string {
	set := make(map[string]struct{}, len(supportedExtensions))
	for ext := range supportedExtensions {
		if !httpExcludedExtensions[ext] {
			set[ext] = struct{}{}
		}
	}
	return sortedKeys(set)
}

func parseMediaType(v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	mt, _, err := mime.ParseMediaType(v)
	if err != nil {
		return "invalid"
	}
	return strings.ToLower(mt)
}

// sanitizeFilename keeps the base name, printable characters only, at most
// 200 bytes. It is metadata only and never touches the file system.
func sanitizeFilename(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	if s == "." || s == "/" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || !utf8.ValidRune(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > 200 {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// wantsJSON reports whether the client asked for JSON and not Markdown.
func wantsJSON(accept string) bool {
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/markdown")
}

func writeConvertError(w http.ResponseWriter, status int, code, msg string) {
	typ := "invalid_request_error"
	if status >= 500 {
		typ = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": typ, "code": code},
	})
}
