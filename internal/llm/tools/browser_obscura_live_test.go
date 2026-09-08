package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// TestBrowserObscuraLiveEndToEnd drives the real browser_* tools end to end
// against a real Obscura CDP server, proving the JS-driven fallbacks in
// browser_jsdriven.go actually work (not just that they build the expected
// JS expressions). It is skipped unless the "obscura" binary is on PATH.
//
// This sandbox has no external network access, so the test only talks to a
// local httptest server, never a public URL.
func TestBrowserObscuraLiveEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("obscura"); err != nil {
		t.Skipf("obscura not found on PATH, skipping live test: %v", err)
	}

	const page = `<!doctype html>
<html>
<head><title>Obscura Live Test</title></head>
<body>
  <h1 id="h">Hello</h1>
  <button id="b" onclick="document.getElementById('h').textContent='Clicked'">Click me</button>
  <input id="i" type="text" oninput="document.getElementById('echo').textContent=this.value" />
  <span id="echo"></span>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	InitBrowserRegistry(&config.InternalToolsConfig{
		BrowserEnabled:     true,
		BrowserType:        "obscura",
		BrowserMaxSessions: 2,
		BrowserTimeout:     30,
	})

	const sessionID = "obscura-live-test-session"
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)
	defer CloseAllBrowserSessions()

	// --- navigate -----------------------------------------------------
	navigateInput, err := json.Marshal(map[string]any{
		"url":      server.URL,
		"wait_for": "#h",
		"timeout":  30,
	})
	if err != nil {
		t.Fatalf("marshal navigate input: %v", err)
	}
	navResp, err := NewBrowserNavigateTool().Run(ctx, ToolCall{ID: "1", Name: BrowserNavigateToolName, Input: string(navigateInput)})
	if err != nil {
		t.Fatalf("browser_navigate returned error: %v", err)
	}
	if navResp.IsError {
		t.Fatalf("browser_navigate failed: %s", navResp.Content)
	}

	// Sanity: the session picked for this pando session must actually be
	// classified as JS-driven, otherwise this test would silently exercise
	// the ordinary chromedp query path instead of the new fallbacks.
	if !browserSessionIsJSDriven(ctx) {
		t.Fatal("expected the obscura browser session to be classified as JS-driven")
	}

	// --- get_content: text --------------------------------------------
	textInput, err := json.Marshal(map[string]any{"format": "text", "selector": "#h"})
	if err != nil {
		t.Fatalf("marshal get_content(text) input: %v", err)
	}
	textResp, err := NewBrowserGetContentTool().Run(ctx, ToolCall{ID: "2", Name: BrowserGetContentToolName, Input: string(textInput)})
	if err != nil {
		t.Fatalf("browser_get_content(text) returned error: %v", err)
	}
	if textResp.IsError {
		t.Fatalf("browser_get_content(text) failed: %s", textResp.Content)
	}
	if got := strings.TrimSpace(textResp.Content); got != "Hello" {
		t.Errorf("browser_get_content(text, #h) = %q, want %q", got, "Hello")
	}

	// --- get_content: html ----------------------------------------------
	htmlInput, err := json.Marshal(map[string]any{"format": "html", "selector": "#h"})
	if err != nil {
		t.Fatalf("marshal get_content(html) input: %v", err)
	}
	htmlResp, err := NewBrowserGetContentTool().Run(ctx, ToolCall{ID: "3", Name: BrowserGetContentToolName, Input: string(htmlInput)})
	if err != nil {
		t.Fatalf("browser_get_content(html) returned error: %v", err)
	}
	if htmlResp.IsError {
		t.Fatalf("browser_get_content(html) failed: %s", htmlResp.Content)
	}
	if !strings.Contains(htmlResp.Content, `id="h"`) || !strings.Contains(htmlResp.Content, "Hello") {
		t.Errorf("browser_get_content(html, #h) = %q, expected it to contain the #h element markup", htmlResp.Content)
	}

	// --- click ----------------------------------------------------------
	clickInput, err := json.Marshal(map[string]any{"selector": "#b"})
	if err != nil {
		t.Fatalf("marshal click input: %v", err)
	}
	clickResp, err := NewBrowserClickTool().Run(ctx, ToolCall{ID: "4", Name: "browser_click", Input: string(clickInput)})
	if err != nil {
		t.Fatalf("browser_click returned error: %v", err)
	}
	if clickResp.IsError {
		t.Fatalf("browser_click failed: %s", clickResp.Content)
	}

	// Confirm the click actually mutated the page (not a false-positive
	// "clicked" from an element that didn't exist).
	postClickInput, err := json.Marshal(map[string]any{"format": "text", "selector": "#h"})
	if err != nil {
		t.Fatalf("marshal post-click get_content input: %v", err)
	}
	postClickResp, err := NewBrowserGetContentTool().Run(ctx, ToolCall{ID: "5", Name: BrowserGetContentToolName, Input: string(postClickInput)})
	if err != nil {
		t.Fatalf("browser_get_content after click returned error: %v", err)
	}
	if got := strings.TrimSpace(postClickResp.Content); got != "Clicked" {
		t.Errorf("after browser_click, #h text = %q, want %q", got, "Clicked")
	}

	// --- fill -------------------------------------------------------------
	const fillValue = "hello obscura"
	fillInput, err := json.Marshal(map[string]any{"selector": "#i", "value": fillValue})
	if err != nil {
		t.Fatalf("marshal fill input: %v", err)
	}
	fillResp, err := NewBrowserFillTool().Run(ctx, ToolCall{ID: "6", Name: "browser_fill", Input: string(fillInput)})
	if err != nil {
		t.Fatalf("browser_fill returned error: %v", err)
	}
	if fillResp.IsError {
		t.Fatalf("browser_fill failed: %s", fillResp.Content)
	}

	// Confirm the fill dispatched a real "input" event: the page's own
	// oninput handler echoes the value into #echo.
	echoInput, err := json.Marshal(map[string]any{"format": "text", "selector": "#echo"})
	if err != nil {
		t.Fatalf("marshal echo get_content input: %v", err)
	}
	echoResp, err := NewBrowserGetContentTool().Run(ctx, ToolCall{ID: "7", Name: BrowserGetContentToolName, Input: string(echoInput)})
	if err != nil {
		t.Fatalf("browser_get_content(#echo) returned error: %v", err)
	}
	if got := strings.TrimSpace(echoResp.Content); got != fillValue {
		t.Errorf("after browser_fill, #echo text = %q, want %q (input event was not dispatched/observed)", got, fillValue)
	}

	// --- screenshot: full page -------------------------------------------
	fullShotInput, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("marshal full screenshot input: %v", err)
	}
	fullShotResp, err := NewBrowserScreenshotTool().Run(ctx, ToolCall{ID: "8", Name: BrowserScreenshotToolName, Input: string(fullShotInput)})
	if err != nil {
		t.Fatalf("browser_screenshot (full page) returned error: %v", err)
	}
	if fullShotResp.IsError {
		t.Fatalf("browser_screenshot (full page) failed: %s", fullShotResp.Content)
	}
	if fullShotResp.Type != ToolResponseTypeImage {
		t.Errorf("browser_screenshot (full page) response type = %q, want %q", fullShotResp.Type, ToolResponseTypeImage)
	}
	assertNonTrivialBase64Image(t, "full page", fullShotResp.Content)

	// --- screenshot: element (exercises jsElementScreenshot) --------------
	elemShotInput, err := json.Marshal(map[string]any{"selector": "#b"})
	if err != nil {
		t.Fatalf("marshal element screenshot input: %v", err)
	}
	elemShotResp, err := NewBrowserScreenshotTool().Run(ctx, ToolCall{ID: "9", Name: BrowserScreenshotToolName, Input: string(elemShotInput)})
	if err != nil {
		t.Fatalf("browser_screenshot (#b) returned error: %v", err)
	}
	if elemShotResp.IsError {
		t.Fatalf("browser_screenshot (#b) failed: %s", elemShotResp.Content)
	}
	if elemShotResp.Type != ToolResponseTypeImage {
		t.Errorf("browser_screenshot (#b) response type = %q, want %q", elemShotResp.Type, ToolResponseTypeImage)
	}
	assertNonTrivialBase64Image(t, "#b element", elemShotResp.Content)
}

// assertNonTrivialBase64Image checks that content decodes as base64 and
// yields a plausible (non-tiny) image payload.
func assertNonTrivialBase64Image(t *testing.T, label, content string) {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		t.Fatalf("%s screenshot content is not valid base64: %v", label, err)
	}
	if len(decoded) < 100 {
		t.Errorf("%s screenshot decoded image is suspiciously small (%d bytes)", label, len(decoded))
	}
}
