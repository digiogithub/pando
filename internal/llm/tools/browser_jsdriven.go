package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// browserNeedsJSDriver reports whether a browser type must be driven through
// plain JavaScript (chromedp.Evaluate) instead of chromedp's high-level query
// actions (WaitVisible, Click, SendKeys, OuterHTML, Text, and the selector
// form of Screenshot).
//
// Obscura (https://github.com/h4ckf0r0day/obscura) never emits the
// DOM.documentUpdated / DOM.setChildNodes CDP events that chromedp's query
// actions rely on to populate their internal node cache (its DOM.getDocument
// also returns root NodeID 0), so every ByQuery-based action blocks until the
// tool call's context deadline is exceeded. Lightpanda and every
// locally-launched Chromium-family engine (chrome, chromium, msedge, opera)
// emit those events normally and keep using the existing chromedp query
// path unchanged.
func browserNeedsJSDriver(browserType string) bool {
	return NormalizeBrowserType(browserType) == "obscura"
}

// browserSessionIsJSDriven reports whether the browser session for the pando
// session ID carried by ctx must be driven through the JS-based fallbacks in
// this file instead of chromedp's query actions. It returns false (the safe
// default: use the normal chromedp query path) when ctx carries no session ID
// or no session has been registered yet for it.
//
// This only takes the registry lock briefly to read the map; it must never be
// called while the caller already holds globalBrowserRegistry.mu, to avoid a
// self-deadlock.
func browserSessionIsJSDriven(pandoCtx context.Context) bool {
	sessionID, _ := GetContextValues(pandoCtx)
	if sessionID == "" {
		return false
	}

	globalBrowserRegistry.mu.Lock()
	sess, ok := globalBrowserRegistry.sessions[sessionID]
	globalBrowserRegistry.mu.Unlock()
	if !ok {
		return false
	}
	return sess.jsDriven
}

// jsDriverWaitTimeout is the default poll timeout passed to jsWaitVisible by
// browser_* tool call sites. It is deliberately generous: the surrounding
// chromedp context (from getBrowserCtxWithTimeout) already carries the
// user/config-configured overall timeout and will cancel ctx.Done() first if
// that elapses, so this value mainly matters when no shorter context deadline
// applies.
const jsDriverWaitTimeout = 30 * time.Second

// jsRect mirrors the plain object returned by getBoundingClientRect() in
// jsElementScreenshotExpr.
type jsRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// encodeJSSelector JSON-encodes a CSS selector so it can be safely embedded
// as a JavaScript string literal inside a generated expression, without any
// risk of injection via quotes/backslashes/newlines in the selector text.
func encodeJSSelector(selector string) (string, error) {
	encoded, err := json.Marshal(selector)
	if err != nil {
		return "", fmt.Errorf("encode selector %q: %w", selector, err)
	}
	return string(encoded), nil
}

// The jsXxxExpr functions below build the JavaScript expression string for
// each JS-driven action, kept separate from the chromedp.Action wrapper so
// they can be unit-tested without a browser (see browser_jsdriven_test.go).

// jsWaitVisibleExpr builds the boolean "is this selector visible" probe
// expression used by jsWaitVisible's poll loop.
func jsWaitVisibleExpr(selector string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  if (!el) { return false; }
  return !!(el.offsetParent !== null || getComputedStyle(el).display !== 'none');
})()`, encodedSelector), nil
}

// jsOuterHTMLExpr builds the expression returning the outerHTML of the
// element matching selector, or null if it does not exist.
func jsOuterHTMLExpr(selector string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  return el ? el.outerHTML : null;
})()`, encodedSelector), nil
}

// jsTextExpr builds the expression returning the rendered text (innerText,
// falling back to textContent) of the element matching selector, or null if
// it does not exist.
func jsTextExpr(selector string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  if (!el) { return null; }
  return (typeof el.innerText === 'string') ? el.innerText : (el.textContent || '');
})()`, encodedSelector), nil
}

// jsClickExpr builds the expression that scrolls the element matching
// selector into view and clicks it, returning true, or false if it does not
// exist.
func jsClickExpr(selector string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  if (!el) { return false; }
  el.scrollIntoView({block: 'center', inline: 'center'});
  el.click();
  return true;
})()`, encodedSelector), nil
}

// jsFillExpr builds the expression that sets the value of an
// <input>/<textarea> (or the text of a contenteditable element) matching
// selector, using the native property setter so framework listeners (React,
// Vue, ...) that hook the setter still fire, then dispatches bubbling
// "input" and "change" events so plain listeners see the change too. Returns
// true, or false if no element matches.
func jsFillExpr(selector, value string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode value: %w", err)
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  if (!el) { return false; }
  var value = %s;
  el.focus();
  if (el.isContentEditable) {
    el.textContent = value;
    el.dispatchEvent(new Event('input', {bubbles: true}));
    el.dispatchEvent(new Event('change', {bubbles: true}));
    return true;
  }
  var tag = el.tagName ? el.tagName.toUpperCase() : '';
  var proto = (tag === 'TEXTAREA') ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  var desc = proto && Object.getOwnPropertyDescriptor(proto, 'value');
  if (desc && desc.set) {
    desc.set.call(el, value);
  } else {
    el.value = value;
  }
  el.dispatchEvent(new Event('input', {bubbles: true}));
  el.dispatchEvent(new Event('change', {bubbles: true}));
  return true;
})()`, encodedSelector, string(encodedValue)), nil
}

// jsElementScreenshotRectExpr builds the expression returning the
// getBoundingClientRect() box ({x, y, w, h}) of the element matching
// selector, or null if it does not exist.
func jsElementScreenshotRectExpr(selector string) (string, error) {
	encodedSelector, err := encodeJSSelector(selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`(function(){
  var el = document.querySelector(%s);
  if (!el) { return null; }
  var r = el.getBoundingClientRect();
  return {x: r.x, y: r.y, w: r.width, h: r.height};
})()`, encodedSelector), nil
}

// jsWaitVisible polls (roughly every 100ms) until the element matching
// selector exists in the DOM and is visible, or returns an error once timeout
// elapses. It replaces chromedp.WaitVisible for JS-driven sessions.
func jsWaitVisible(selector string, timeout time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr, err := jsWaitVisibleExpr(selector)
		if err != nil {
			return err
		}

		deadline := time.Now().Add(timeout)
		for {
			var visible bool
			if err := chromedp.Evaluate(expr, &visible).Do(ctx); err != nil {
				return fmt.Errorf("jsWaitVisible %q: %w", selector, err)
			}
			if visible {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("jsWaitVisible: timed out after %s waiting for %q to become visible", timeout, selector)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// jsOuterHTML reads the outerHTML of the element matching selector. For an
// empty selector or the special selector "html" it falls back to
// document.documentElement.outerHTML (the whole page), matching the
// behaviour callers expect from the chromedp.OuterHTML query action it
// replaces. It errors if no element matches a non-html selector.
func jsOuterHTML(selector string, out *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		trimmed := strings.TrimSpace(selector)
		if trimmed == "" || strings.EqualFold(trimmed, "html") {
			return chromedp.Evaluate(`document.documentElement.outerHTML`, out).Do(ctx)
		}

		expr, err := jsOuterHTMLExpr(selector)
		if err != nil {
			return err
		}
		if err := chromedp.Evaluate(expr, out).Do(ctx); err != nil {
			if errors.Is(err, chromedp.ErrJSNull) {
				return fmt.Errorf("jsOuterHTML: no element found for selector %q", selector)
			}
			return fmt.Errorf("jsOuterHTML %q: %w", selector, err)
		}
		return nil
	})
}

// jsText reads the rendered text (innerText, falling back to textContent for
// elements that don't expose innerText, e.g. SVG nodes) of the element
// matching selector. It errors if no element matches.
func jsText(selector string, out *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr, err := jsTextExpr(selector)
		if err != nil {
			return err
		}
		if err := chromedp.Evaluate(expr, out).Do(ctx); err != nil {
			if errors.Is(err, chromedp.ErrJSNull) {
				return fmt.Errorf("jsText: no element found for selector %q", selector)
			}
			return fmt.Errorf("jsText %q: %w", selector, err)
		}
		return nil
	})
}

// jsClick scrolls the element matching selector into view and clicks it via
// plain JavaScript. It errors if no element matches.
func jsClick(selector string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr, err := jsClickExpr(selector)
		if err != nil {
			return err
		}
		var clicked bool
		if err := chromedp.Evaluate(expr, &clicked).Do(ctx); err != nil {
			return fmt.Errorf("jsClick %q: %w", selector, err)
		}
		if !clicked {
			return fmt.Errorf("jsClick: no element found for selector %q", selector)
		}
		return nil
	})
}

// jsFill sets the value of an <input>/<textarea> (or the text of a
// contenteditable element) matching selector, using the native property
// setter so framework listeners (React, Vue, ...) that hook the setter still
// fire, then dispatches bubbling "input" and "change" events so plain
// listeners see the change too. It errors if no element matches.
func jsFill(selector, value string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr, err := jsFillExpr(selector, value)
		if err != nil {
			return err
		}
		var filled bool
		if err := chromedp.Evaluate(expr, &filled).Do(ctx); err != nil {
			return fmt.Errorf("jsFill %q: %w", selector, err)
		}
		if !filled {
			return fmt.Errorf("jsFill: no element found for selector %q", selector)
		}
		return nil
	})
}

// jsElementScreenshot captures a screenshot clipped to the element matching
// selector. It reads the element's box via getBoundingClientRect() (a plain
// JS call, unlike chromedp.Screenshot which depends on the query node cache)
// and then asks the CDP page domain to capture that clip directly, exactly
// like the chromedp.Screenshot query action would for a Chromium-family
// browser. It errors if no element matches or the element has no visible
// size.
func jsElementScreenshot(selector string, buf *[]byte, quality int) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr, err := jsElementScreenshotRectExpr(selector)
		if err != nil {
			return err
		}

		var rect *jsRect
		if err := chromedp.Evaluate(expr, &rect).Do(ctx); err != nil {
			return fmt.Errorf("jsElementScreenshot %q: %w", selector, err)
		}
		if rect == nil || rect.W <= 0 || rect.H <= 0 {
			return fmt.Errorf("jsElementScreenshot: no visible element found for selector %q", selector)
		}

		format := page.CaptureScreenshotFormatPng
		if quality != 100 {
			format = page.CaptureScreenshotFormatJpeg
		}
		clip := &page.Viewport{X: rect.X, Y: rect.Y, Width: rect.W, Height: rect.H, Scale: 1}
		data, err := page.CaptureScreenshot().
			WithClip(clip).
			WithCaptureBeyondViewport(true).
			WithFromSurface(true).
			WithFormat(format).
			WithQuality(int64(quality)).
			Do(ctx)
		if err != nil {
			return fmt.Errorf("jsElementScreenshot %q: capture: %w", selector, err)
		}
		*buf = data
		return nil
	})
}
