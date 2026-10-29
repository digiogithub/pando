package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func runViewForTest(t *testing.T, path string) ToolResponse {
	t.Helper()
	input, err := json.Marshal(map[string]string{"file_path": path})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	resp, err := (&viewTool{}).Run(context.Background(), ToolCall{Name: "view", Input: string(input)})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return resp
}

func TestViewReturnsPNGAsImageResponse(t *testing.T) {
	data := makeTestPNG(t)
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	resp := runViewForTest(t, path)
	if resp.IsError {
		t.Fatalf("expected image response, got error: %s", resp.Content)
	}
	if resp.Type != ToolResponseTypeImage {
		t.Fatalf("type = %q, want %q", resp.Type, ToolResponseTypeImage)
	}
	raw, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		t.Fatalf("content is not base64: %v", err)
	}
	if !bytes.Equal(raw, data) {
		t.Fatal("image bytes do not round-trip through the response")
	}
}

func TestViewImageContentIsDetectedByBytesNotExtension(t *testing.T) {
	// A JPEG saved with a .png extension must still be accepted: the MIME type
	// comes from the content, so the extension is not the source of truth.
	data := makeTestPNG(t)
	path := filepath.Join(t.TempDir(), "not-really.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := runViewForTest(t, path); resp.IsError || resp.Type != ToolResponseTypeImage {
		t.Fatalf("expected image response for png content with .jpg extension, got %+v", resp)
	}
}

func TestViewRejectsImageExtensionWithNonImageContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.png")
	if err := os.WriteFile(path, []byte("this is plain text, not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := runViewForTest(t, path)
	if !resp.IsError {
		t.Fatalf("expected error response, got type %q", resp.Type)
	}
	if !strings.Contains(resp.Content, "not a supported image") {
		t.Fatalf("unexpected error text: %s", resp.Content)
	}
}
