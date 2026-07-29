package acp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: uint8((x * y) % 239), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestReadJSONRPCLineHandlesHugeLine(t *testing.T) {
	huge := strings.Repeat("a", 5*1024*1024)
	in := strings.NewReader(`{"id":1,"big":"` + huge + `"}` + "\n" + `{"id":2}` + "\n")
	r := bufio.NewReaderSize(in, 4096)

	line, err := readJSONRPCLine(r, 128*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(line) < len(huge) {
		t.Fatalf("truncated line: got %d bytes", len(line))
	}

	line, err = readJSONRPCLine(r, 128*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error on second line: %v", err)
	}
	if !strings.Contains(string(line), `"id":2`) {
		t.Fatalf("reader lost sync: %q", string(line))
	}
}

func TestReadJSONRPCLineTooLongKeepsStreamInSync(t *testing.T) {
	oversized := strings.Repeat("b", 200*1024)
	in := strings.NewReader(`{"id":7,"big":"` + oversized + `"}` + "\n" + `{"id":8}` + "\n")
	r := bufio.NewReaderSize(in, 4096)

	line, err := readJSONRPCLine(r, 64*1024)
	if err != errLineTooLong {
		t.Fatalf("expected errLineTooLong, got %v", err)
	}
	if id := peekRequestID(line); string(id) != "7" {
		t.Fatalf("expected id 7 from truncated prefix, got %q", string(id))
	}

	line, err = readJSONRPCLine(r, 64*1024)
	if err != nil {
		t.Fatalf("unexpected error after oversized line: %v", err)
	}
	if !strings.Contains(string(line), `"id":8`) {
		t.Fatalf("reader lost sync after oversized line: %q", string(line))
	}

	if _, err = readJSONRPCLine(r, 64*1024); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestPeekRequestIDStringAndNumeric(t *testing.T) {
	if got := peekRequestID([]byte(`{"jsonrpc":"2.0","id":"abc-1","method":"x"`)); string(got) != `"abc-1"` {
		t.Fatalf("string id: got %q", string(got))
	}
	if got := peekRequestID([]byte(`{"jsonrpc":"2.0","id":42,"method":"x"`)); string(got) != "42" {
		t.Fatalf("numeric id: got %q", string(got))
	}
	if got := peekRequestID([]byte(`{"jsonrpc":"2.0","method":"x"}`)); got != nil {
		t.Fatalf("expected nil id, got %q", string(got))
	}
}

func TestShrinkPromptImagesReducesPayload(t *testing.T) {
	big := testPNG(t, 3000, 2000)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": "s1",
			"prompt": []any{
				map[string]any{"type": "text", "text": "what is this?"},
				map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(big)},
			},
		},
	}
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	shrunk, err := shrinkPromptImages(line, 512*1024)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if len(shrunk) >= len(line) {
		t.Fatalf("payload not reduced: %d -> %d", len(line), len(shrunk))
	}

	var out map[string]any
	if err := json.Unmarshal(shrunk, &out); err != nil {
		t.Fatalf("shrunk payload is not valid JSON: %v", err)
	}
	params := out["params"].(map[string]any)
	blocks := params["prompt"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	img := blocks[1].(map[string]any)
	data, err := base64.StdEncoding.DecodeString(img["data"].(string))
	if err != nil {
		t.Fatalf("shrunk image is not valid base64: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("shrunk image is empty")
	}
	if out["method"] != "session/prompt" {
		t.Fatalf("method lost during rewrite: %v", out["method"])
	}
}

func TestShrinkPromptImagesWithoutImages(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"prompt":[{"type":"text","text":"hi"}]}}`)
	if _, err := shrinkPromptImages(line, 1024); err == nil {
		t.Fatal("expected an error when there is no image to shrink")
	}
}
