package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// contextWithSessionID and contextWithoutSessionID build minimal pando
// contexts for exercising browserSessionIsJSDriven without a real tool call.
func contextWithSessionID(t *testing.T, sessionID string) context.Context {
	t.Helper()
	return context.WithValue(context.Background(), SessionIDContextKey, sessionID)
}

func contextWithoutSessionID(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestBrowserNeedsJSDriver(t *testing.T) {
	tests := []struct {
		name        string
		browserType string
		want        bool
	}{
		{name: "obscura", browserType: "obscura", want: true},
		{name: "obscura alias casing", browserType: "Obscura-Browser", want: true},
		{name: "lightpanda stays on chromedp query path", browserType: "lightpanda", want: false},
		{name: "chrome stays on chromedp query path", browserType: "chrome", want: false},
		{name: "chromium stays on chromedp query path", browserType: "chromium", want: false},
		{name: "msedge stays on chromedp query path", browserType: "msedge", want: false},
		{name: "opera stays on chromedp query path", browserType: "opera", want: false},
		{name: "empty", browserType: "", want: false},
		{name: "unknown", browserType: "some-future-browser", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserNeedsJSDriver(tt.browserType); got != tt.want {
				t.Errorf("browserNeedsJSDriver(%q) = %v, want %v", tt.browserType, got, tt.want)
			}
		})
	}
}

// jsonEscapedSelector returns how encoding/json would render selector as a
// JS/JSON string literal, so tests can assert the expression builders use
// the escaped form (never a raw string concatenation).
func jsonEscapedSelector(t *testing.T, selector string) string {
	t.Helper()
	encoded, err := json.Marshal(selector)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", selector, err)
	}
	return string(encoded)
}

func TestBrowserEncodeJSSelector(t *testing.T) {
	tests := []string{
		`div.card`,
		`#main`,
		`input[name="q"]`,
		`h1"); alert(1); //`,
		`sel'with"both\backslash`,
		"",
	}
	for _, selector := range tests {
		t.Run(selector, func(t *testing.T) {
			got, err := encodeJSSelector(selector)
			if err != nil {
				t.Fatalf("encodeJSSelector(%q) returned error: %v", selector, err)
			}
			want := jsonEscapedSelector(t, selector)
			if got != want {
				t.Errorf("encodeJSSelector(%q) = %s, want %s", selector, got, want)
			}
			// The encoded form must be a JSON/JS string literal (quoted),
			// never the raw selector spliced in unescaped.
			if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
				t.Errorf("encodeJSSelector(%q) = %s, expected a quoted JSON string literal", selector, got)
			}
		})
	}
}

func TestBrowserJSWaitVisibleExpr(t *testing.T) {
	const selector = `button[data-testid="submit"]`
	expr, err := jsWaitVisibleExpr(selector)
	if err != nil {
		t.Fatalf("jsWaitVisibleExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	if !strings.Contains(expr, "offsetParent") || !strings.Contains(expr, "getComputedStyle") {
		t.Errorf("expression does not check visibility via offsetParent/getComputedStyle: %s", expr)
	}
}

func TestBrowserJSOuterHTMLExpr(t *testing.T) {
	const selector = `article#post-1`
	expr, err := jsOuterHTMLExpr(selector)
	if err != nil {
		t.Fatalf("jsOuterHTMLExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	if !strings.Contains(expr, ".outerHTML") {
		t.Errorf("expression does not read outerHTML: %s", expr)
	}
}

func TestBrowserJSTextExpr(t *testing.T) {
	const selector = `.summary`
	expr, err := jsTextExpr(selector)
	if err != nil {
		t.Fatalf("jsTextExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	if !strings.Contains(expr, "innerText") || !strings.Contains(expr, "textContent") {
		t.Errorf("expression does not read innerText with textContent fallback: %s", expr)
	}
}

func TestBrowserJSClickExpr(t *testing.T) {
	const selector = `button.primary`
	expr, err := jsClickExpr(selector)
	if err != nil {
		t.Fatalf("jsClickExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	if !strings.Contains(expr, "scrollIntoView") {
		t.Errorf("expression does not scroll the element into view: %s", expr)
	}
	if !strings.Contains(expr, ".click()") {
		t.Errorf("expression does not click the element: %s", expr)
	}
}

func TestBrowserJSFillExpr(t *testing.T) {
	const selector = `input#email`
	const value = `it's "quoted" & tricky`
	expr, err := jsFillExpr(selector, value)
	if err != nil {
		t.Fatalf("jsFillExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	wantValue := jsonEscapedSelector(t, value) // json.Marshal escaping applies to any string
	if !strings.Contains(expr, "var value = "+wantValue) {
		t.Errorf("expression does not JSON-escape the fill value: %s", expr)
	}
	if !strings.Contains(expr, "getOwnPropertyDescriptor") {
		t.Errorf("expression does not use the native value property descriptor: %s", expr)
	}
	if !strings.Contains(expr, "HTMLInputElement.prototype") || !strings.Contains(expr, "HTMLTextAreaElement.prototype") {
		t.Errorf("expression does not branch between input and textarea prototypes: %s", expr)
	}
	if !strings.Contains(expr, "isContentEditable") {
		t.Errorf("expression does not handle contenteditable elements: %s", expr)
	}
	if !strings.Contains(expr, "dispatchEvent(new Event('input'") || !strings.Contains(expr, "dispatchEvent(new Event('change'") {
		t.Errorf("expression does not dispatch bubbling input/change events: %s", expr)
	}
	if !strings.Contains(expr, "bubbles: true") {
		t.Errorf("expression events are not marked bubbling: %s", expr)
	}
}

func TestBrowserJSFillExprEscapesInjectionAttempt(t *testing.T) {
	// A value that would break out of a naive string-concatenated JS literal
	// must come back JSON-escaped, never spliced in raw.
	const selector = `#i`
	const value = `"); document.title = 'pwned'; //`
	expr, err := jsFillExpr(selector, value)
	if err != nil {
		t.Fatalf("jsFillExpr: %v", err)
	}
	// A naive `"var value = \"" + value + "\";"` concatenation would leave
	// the closing quote of the value unescaped, producing this exact
	// substring right after the assignment. The JSON encoder must escape
	// that inner quote instead.
	if strings.Contains(expr, `var value = "");`) {
		t.Errorf("value appears to have been spliced in unescaped (JS injection possible): %s", expr)
	}
	wantValue := jsonEscapedSelector(t, value)
	if !strings.Contains(expr, "var value = "+wantValue) {
		t.Errorf("expression does not contain the JSON-escaped value literal: %s", expr)
	}
	if !strings.Contains(expr, `\");`) {
		t.Errorf("expected the embedded quote to be JSON-escaped as \\\": %s", expr)
	}
}

func TestBrowserJSElementScreenshotRectExpr(t *testing.T) {
	const selector = `#hero-image`
	expr, err := jsElementScreenshotRectExpr(selector)
	if err != nil {
		t.Fatalf("jsElementScreenshotRectExpr: %v", err)
	}

	wantSelector := jsonEscapedSelector(t, selector)
	if !strings.Contains(expr, "document.querySelector("+wantSelector+")") {
		t.Errorf("expression does not contain JSON-escaped querySelector call: %s", expr)
	}
	if !strings.Contains(expr, "getBoundingClientRect") {
		t.Errorf("expression does not read getBoundingClientRect: %s", expr)
	}
	for _, field := range []string{`x:`, `y:`, `w:`, `h:`} {
		if !strings.Contains(expr, field) {
			t.Errorf("expression does not return field %q: %s", field, expr)
		}
	}
}

func TestBrowserSessionIsJSDrivenNoSessionID(t *testing.T) {
	// No session ID in context at all: must return false without touching
	// the registry map in a way that could deadlock or panic.
	if browserSessionIsJSDriven(contextWithoutSessionID(t)) {
		t.Error("expected false when context carries no session ID")
	}
}

func TestBrowserSessionIsJSDrivenUnknownSession(t *testing.T) {
	globalBrowserRegistry.mu.Lock()
	delete(globalBrowserRegistry.sessions, "no-such-session")
	globalBrowserRegistry.mu.Unlock()

	ctx := contextWithSessionID(t, "no-such-session")
	if browserSessionIsJSDriven(ctx) {
		t.Error("expected false for a session ID with no registered session")
	}
}

func TestBrowserSessionIsJSDrivenRegisteredSession(t *testing.T) {
	const sessionID = "jsdriven-unit-test-session"

	globalBrowserRegistry.mu.Lock()
	globalBrowserRegistry.sessions[sessionID] = &browserSession{jsDriven: true}
	globalBrowserRegistry.mu.Unlock()
	defer func() {
		globalBrowserRegistry.mu.Lock()
		delete(globalBrowserRegistry.sessions, sessionID)
		globalBrowserRegistry.mu.Unlock()
	}()

	if !browserSessionIsJSDriven(contextWithSessionID(t, sessionID)) {
		t.Error("expected true for a session registered with jsDriven=true")
	}
}
