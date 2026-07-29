package acp

import (
	"encoding/base64"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func testAgent() *PandoACPAgent {
	return &PandoACPAgent{logger: log.New(io.Discard, "", 0)}
}

func TestExtractPromptContentImageOnly(t *testing.T) {
	a := testAgent()
	img := testPNG(t, 8, 8)
	text, atts, err := a.extractPromptContent([]acpsdk.ContentBlock{
		acpsdk.ImageBlock(base64.StdEncoding.EncodeToString(img), "image/png"),
	})
	if err != nil {
		t.Fatalf("image-only prompt rejected: %v", err)
	}
	if text != imageOnlyPromptText {
		t.Fatalf("unexpected placeholder text: %q", text)
	}
	if len(atts) != 1 || len(atts[0].Content) != len(img) {
		t.Fatalf("expected the image attachment, got %+v", atts)
	}
}

func TestExtractPromptContentResourceLinkImage(t *testing.T) {
	a := testAgent()
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	img := testPNG(t, 6, 6)
	if err := os.WriteFile(path, img, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	mime := "image/png"
	block := acpsdk.ResourceLinkBlock("shot.png", "file://"+path)
	block.ResourceLink.MimeType = &mime

	text, atts, err := a.extractPromptContent([]acpsdk.ContentBlock{
		acpsdk.TextBlock("describe"),
		block,
	})
	if err != nil {
		t.Fatalf("resource_link prompt rejected: %v", err)
	}
	if text != "describe" {
		t.Fatalf("unexpected text: %q", text)
	}
	if len(atts) != 1 {
		t.Fatalf("expected the linked image to be inlined, got %d attachments", len(atts))
	}
	if atts[0].FilePath != path || atts[0].FileName != "shot.png" || atts[0].MimeType != "image/png" {
		t.Fatalf("unexpected attachment metadata: %+v", atts[0])
	}
}

func TestExtractPromptContentResourceLinkNonImageFallsBackToText(t *testing.T) {
	a := testAgent()
	block := acpsdk.ResourceLinkBlock("notes.md", "file:///tmp/does-not-exist/notes.md")

	text, atts, err := a.extractPromptContent([]acpsdk.ContentBlock{block})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("expected no attachment, got %d", len(atts))
	}
	if !strings.Contains(text, "notes.md") {
		t.Fatalf("expected the file to be mentioned as text, got %q", text)
	}
}

func TestExtractPromptContentEmbeddedResources(t *testing.T) {
	a := testAgent()
	img := testPNG(t, 5, 5)
	mime := "image/png"

	blocks := []acpsdk.ContentBlock{
		acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{
			TextResourceContents: &acpsdk.TextResourceContents{Uri: "file:///proj/main.go", Text: "package main"},
		}),
		acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{
			BlobResourceContents: &acpsdk.BlobResourceContents{
				Uri:      "file:///proj/shot.png",
				MimeType: &mime,
				Blob:     base64.StdEncoding.EncodeToString(img),
			},
		}),
	}

	text, atts, err := a.extractPromptContent(blocks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "package main") {
		t.Fatalf("embedded text resource lost: %q", text)
	}
	if len(atts) != 1 || atts[0].FileName != "shot.png" {
		t.Fatalf("embedded image resource not attached: %+v", atts)
	}
}

func TestExtractPromptContentEmptyStillFails(t *testing.T) {
	a := testAgent()
	if _, _, err := a.extractPromptContent(nil); err == nil {
		t.Fatal("expected an error for an empty prompt")
	}
}
