package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner records what it was asked and answers md or err after delay.
type fakeRunner struct {
	md    string
	err   error
	delay time.Duration
	ext   string
	data  []byte
}

func (f *fakeRunner) Run(ctx context.Context, ext string, data []byte, maxOutput int64) (string, error) {
	f.ext, f.data = ext, data
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.err != nil {
		return "", f.err
	}
	if maxOutput > 0 && int64(len(f.md)) > maxOutput {
		return "", ErrOutputTooLarge
	}
	return f.md, nil
}

func post(h http.Handler, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/convert", bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body is not JSON: %s", rec.Body)
	}
	return e.Error.Code
}

func TestConvertEndpointRawBodyCSV(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sample.csv"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPHandler(HTTPOptions{Runner: InProcessRunner{Converter: New()}})
	rec := post(h, data, map[string]string{"Content-Type": "text/csv", "X-Filename": "../../etc/sample.csv", "Accept": "text/markdown, text/plain"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Pando") {
		t.Fatalf("markdown missing content: %s", rec.Body)
	}
	if rec.Header().Get("X-Pando-Convert-Format") != "csv" {
		t.Fatalf("format header %q", rec.Header().Get("X-Pando-Convert-Format"))
	}
}

func TestConvertEndpointMultipartJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sample.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("note", "ignored")
	fw, _ := mw.CreateFormFile("file", "dir/sample.xlsx")
	_, _ = fw.Write(data)
	_ = mw.Close()

	h := NewHTTPHandler(HTTPOptions{Runner: InProcessRunner{Converter: New()}})
	rec := post(h, body.Bytes(), map[string]string{"Content-Type": mw.FormDataContentType(), "Accept": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp ConvertResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Markdown, "42") {
		t.Fatalf("markdown missing cells: %s", resp.Markdown)
	}
	m := resp.Metadata
	if m.Format != "xlsx" || m.Filename != "sample.xlsx" || m.InputBytes != len(data) || m.MarkdownBytes != len(resp.Markdown) {
		t.Fatalf("metadata %+v", m)
	}
}

func TestConvertEndpointFormatResolution(t *testing.T) {
	cases := []struct {
		ct, name, wantExt string
		wantStatus        int
	}{
		{"application/pdf", "", ".pdf", 200},
		{"application/pdf; name=x.pdf", "", ".pdf", 200},
		{"application/octet-stream", "Report.DOCX", ".docx", 200},
		{"", "page.htm", ".html", 200},
		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "", ".pptx", 200},
		{"application/zip", "a.zip", "", 415},
		{"application/octet-stream", "a.zip", "", 415},
		{"application/msword", "a.doc", "", 415},
		{"application/octet-stream", "a.exe", "", 415},
		{"image/png", "a.pdf", "", 415},
		{"%%%", "a.pdf", "", 415},
	}
	for _, c := range cases {
		f := &fakeRunner{md: "# ok"}
		h := NewHTTPHandler(HTTPOptions{Runner: f})
		rec := post(h, []byte("doc"), map[string]string{"Content-Type": c.ct, "X-Filename": c.name})
		if rec.Code != c.wantStatus || f.ext != c.wantExt {
			t.Errorf("ct=%q name=%q: status %d ext %q, want %d %q", c.ct, c.name, rec.Code, f.ext, c.wantStatus, c.wantExt)
		}
	}
}

func TestConvertEndpointLimits(t *testing.T) {
	t.Run("input too large", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{MaxInputBytes: 10, Runner: &fakeRunner{md: "x"}})
		rec := post(h, bytes.Repeat([]byte("a"), 11), map[string]string{"Content-Type": "text/plain"})
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d", rec.Code)
		}
		// Chunked body (no Content-Length) is cut by MaxBytesReader.
		req := httptest.NewRequest(http.MethodPost, "/v1/convert", io.MultiReader(strings.NewReader(strings.Repeat("a", 20))))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "text/plain")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("chunked status %d", rec.Code)
		}
	})
	t.Run("output too large", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{MaxOutputBytes: 3, Runner: &fakeRunner{md: "toolong"}})
		rec := post(h, []byte("doc"), map[string]string{"Content-Type": "text/plain"})
		if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "output_too_large" {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{Timeout: 20 * time.Millisecond, Runner: &fakeRunner{md: "x", delay: time.Second}})
		rec := post(h, []byte("doc"), map[string]string{"Content-Type": "text/plain"})
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("status %d", rec.Code)
		}
	})
	t.Run("busy", func(t *testing.T) {
		slow := &fakeRunner{md: "x", delay: 300 * time.Millisecond}
		h := NewHTTPHandler(HTTPOptions{Timeout: 50 * time.Millisecond, MaxConcurrent: 1, Runner: slow})
		hh := h.(*httpHandler)
		hh.slots <- struct{}{} // occupy the only slot
		defer func() { <-hh.slots }()
		rec := post(h, []byte("doc"), map[string]string{"Content-Type": "text/plain"})
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("status %d", rec.Code)
		}
	})
	t.Run("empty", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{Runner: &fakeRunner{md: "x"}})
		if rec := post(h, nil, map[string]string{"Content-Type": "application/pdf"}); rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d", rec.Code)
		}
	})
	t.Run("conversion failure does not echo content", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{Runner: &fakeRunner{err: fmt.Errorf("%w: secret-content", ErrConversionFailed)}})
		rec := post(h, []byte("doc"), map[string]string{"Content-Type": "application/pdf"})
		if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "secret-content") {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	})
	t.Run("method", func(t *testing.T) {
		h := NewHTTPHandler(HTTPOptions{Runner: &fakeRunner{}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/convert", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "passwd",
		`C:\Users\x\a.pdf`:       "a.pdf",
		"a\r\nb.pdf":             "ab.pdf",
		"":                       "",
		strings.Repeat("é", 300): strings.Repeat("é", 100),
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// helperRunner re-executes this test binary as the conversion child; the
// mode selects how the fake child behaves (see TestHelperConvertProcess).
func helperRunner(mode string) ProcessRunner {
	return ProcessRunner{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestHelperConvertProcess$", "--"},
		Env:        append(MinimalEnv(""), "PANDO_CONVERT_HELPER="+mode),
	}
}

// TestHelperConvertProcess is not a test: it is the child process of the
// ProcessRunner tests, standing in for `pando convert --stdin-format <ext>`.
func TestHelperConvertProcess(t *testing.T) {
	mode := os.Getenv("PANDO_CONVERT_HELPER")
	if mode == "" {
		return
	}
	ext := os.Args[len(os.Args)-1]
	switch mode {
	case "convert":
		data, _ := io.ReadAll(os.Stdin)
		md, err := New().ConvertReader(bytes.NewReader(data), "stdin"+ext)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(md)
	case "env":
		fmt.Println(strings.Join(os.Environ(), "\n"))
	case "hang":
		time.Sleep(30 * time.Second)
	case "flood":
		for i := 0; i < 1<<20; i++ {
			fmt.Print("xxxxxxxxxxxxxxxx")
		}
	case "fail":
		fmt.Fprintln(os.Stderr, "boom: broken document")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestProcessRunner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sample.csv"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	md, err := helperRunner("convert").Run(ctx, ".csv", data, 1<<20)
	if err != nil || !strings.Contains(md, "Pando") || strings.HasSuffix(md, "\n\n") {
		t.Fatalf("convert: %q %v", md, err)
	}

	t.Setenv("PANDO_SECRET_FOR_TEST", "s3cret")
	out, err := helperRunner("env").Run(ctx, ".csv", data, 1<<20)
	if err != nil || strings.Contains(out, "s3cret") {
		t.Fatalf("child environment leaks the parent's: %v\n%s", err, out)
	}

	tctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := helperRunner("hang").Run(tctx, ".csv", data, 1<<20); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hang: err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("hung child was not killed")
	}

	if _, err := helperRunner("flood").Run(ctx, ".csv", data, 1024); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("flood: err = %v, want ErrOutputTooLarge", err)
	}

	if _, err := helperRunner("fail").Run(ctx, ".csv", data, 1024); !errors.Is(err, ErrConversionFailed) {
		t.Fatalf("fail: err = %v, want ErrConversionFailed", err)
	}
}
