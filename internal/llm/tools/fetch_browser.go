package tools

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// fetchBrowser is an interface for browser-based URL fetching.
type fetchBrowser interface {
	Name() string
	Fetch(url string) ([]byte, error)
}

// htmlCleaningOptions configures what elements to remove from fetched HTML.
type htmlCleaningOptions struct {
	KeepHeader   bool
	KeepFooter   bool
	KeepNav      bool
	KeepStyles   bool
	KeepComments bool
}

func defaultHTMLCleaningOptions() *htmlCleaningOptions {
	return &htmlCleaningOptions{
		KeepHeader:   false,
		KeepFooter:   false,
		KeepNav:      false,
		KeepStyles:   false,
		KeepComments: false,
	}
}

// executableFinder locates a binary on PATH, trying each name in order.
type executableFinder struct {
	names []string
}

func (f *executableFinder) find() (string, error) {
	for _, name := range f.names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no executable found for: %v", f.names)
}

// ─── Chrome ────────────────────────────────────────────────────────────────

type chromeBrowser struct {
	execPath    string
	cleaningOpts *htmlCleaningOptions
}

func newChromeBrowser() (*chromeBrowser, error) {
	finder := &executableFinder{
		names: []string{"google-chrome", "chromium", "chromium-browser"},
	}
	path, err := finder.find()
	if err != nil {
		return nil, err
	}
	return &chromeBrowser{execPath: path, cleaningOpts: defaultHTMLCleaningOptions()}, nil
}

func (c *chromeBrowser) Name() string { return "Chrome/Chromium" }

func (c *chromeBrowser) Fetch(url string) ([]byte, error) {
	cmd := exec.Command(c.execPath,
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--enable-automation",
		"--virtual-time-budget=5000", // allow 5 s for JS execution
		"--dump-dom",
		url,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("chrome execution error: %v", err)
	}
	return cleanHTMLContent(output, c.cleaningOpts), nil
}

// ─── Firefox ───────────────────────────────────────────────────────────────

type firefoxBrowser struct {
	execPath    string
	cleaningOpts *htmlCleaningOptions
}

func newFirefoxBrowser() (*firefoxBrowser, error) {
	finder := &executableFinder{names: []string{"firefox"}}
	path, err := finder.find()
	if err != nil {
		return nil, err
	}
	return &firefoxBrowser{execPath: path, cleaningOpts: defaultHTMLCleaningOptions()}, nil
}

func (f *firefoxBrowser) Name() string { return "Firefox" }

func (f *firefoxBrowser) Fetch(url string) ([]byte, error) {
	cmd := exec.Command(f.execPath,
		"--headless",
		"--enable-automation",
		"--wait-for-browser",
		"--dump-dom",
		url,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("firefox execution error: %v", err)
	}
	return cleanHTMLContent(output, f.cleaningOpts), nil
}

// ─── Curl ──────────────────────────────────────────────────────────────────

type curlBrowser struct {
	execPath    string
	cleaningOpts *htmlCleaningOptions
}

func newCurlBrowser() (*curlBrowser, error) {
	finder := &executableFinder{names: []string{"curl"}}
	path, err := finder.find()
	if err != nil {
		return nil, err
	}
	return &curlBrowser{execPath: path, cleaningOpts: defaultHTMLCleaningOptions()}, nil
}

func (c *curlBrowser) Name() string { return "Curl" }

func (c *curlBrowser) Fetch(url string) ([]byte, error) {
	cmd := exec.Command(c.execPath,
		"-L",        // follow redirects
		"-s",        // silent
		"-A", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"--max-time", "30",
		url,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("curl execution error: %v", err)
	}
	return cleanHTMLContent(output, c.cleaningOpts), nil
}

// ─── Browser factory ───────────────────────────────────────────────────────

// defaultBrowserOrder defines the priority when browser="auto".
var defaultBrowserOrder = []string{"chrome", "firefox", "curl"}

// newFetchBrowser creates a browser instance by name.
func newFetchBrowser(name string) (fetchBrowser, error) {
	switch strings.ToLower(name) {
	case "chrome", "chromium":
		return newChromeBrowser()
	case "firefox":
		return newFirefoxBrowser()
	case "curl":
		return newCurlBrowser()
	default:
		return nil, fmt.Errorf("unsupported browser: %q", name)
	}
}

// getDefaultFetchBrowser tries browsers in order and returns the first available.
func getDefaultFetchBrowser() (fetchBrowser, error) {
	var lastErr error
	for _, name := range defaultBrowserOrder {
		b, err := newFetchBrowser(name)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no supported browser found: %v", lastErr)
}

// ─── HTML cleaner ──────────────────────────────────────────────────────────

// cleanHTMLContent strips scripts, ads, inline JS, navigation, and other
// noise from raw HTML, mirroring the md-fetch approach.
func cleanHTMLContent(content []byte, opts *htmlCleaningOptions) []byte {
	contentStr := string(content)

	// Remove <script> tags
	scriptPattern := regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
	contentStr = scriptPattern.ReplaceAllString(contentStr, "")

	// Remove <style> tags unless kept
	if !opts.KeepStyles {
		stylePattern := regexp.MustCompile(`(?is)<style.*?>.*?</style>`)
		contentStr = stylePattern.ReplaceAllString(contentStr, "")
	}

	// Remove HTML comments unless kept
	if !opts.KeepComments {
		commentPattern := regexp.MustCompile(`(?is)<!--.*?-->`)
		contentStr = commentPattern.ReplaceAllString(contentStr, "")
	}

	// Remove inline JS event attributes
	jsAttrPattern := regexp.MustCompile(`(?i)(on\w+)="[^"]*"`)
	contentStr = jsAttrPattern.ReplaceAllString(contentStr, "")

	// Strip remaining JS patterns
	jsPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?s)var\s+_g\s*=\s*\{\s*kEI\s*:[^}]*\}\s*;`),
		regexp.MustCompile(`(?s)var\s+google\s*=\s*\{\s*[^}]*\}\s*;`),
		regexp.MustCompile(`(?m)^[ \t]*window\.[a-zA-Z_$][0-9a-zA-Z_$]*\s*=\s*[^;]*;`),
		regexp.MustCompile(`(?m)^[ \t]*var\s+[a-zA-Z_$][0-9a-zA-Z_$]*\s*=[^;]*;`),
		regexp.MustCompile(`(?s)\(\s*function\s*\([^)]*\)\s*\{[^}]*\}\s*\)\s*\([^)]*\)\s*;?`),
		regexp.MustCompile(`(?s)\(\s*function\s*\(\)\s*\{\s*var\s+[a-zA-Z_$][0-9a-zA-Z_$]*\s*=\s*\{[^}]*\}\s*;[^}]*\}\s*\)\s*\(\s*\)\s*;`),
		regexp.MustCompile(`(?s)document\.[a-zA-Z_$][0-9a-zA-Z_$]*\.addEventListener\s*\([^)]*\)\s*;`),
	}
	for _, p := range jsPatterns {
		contentStr = p.ReplaceAllString(contentStr, "")
	}

	doc, err := html.Parse(strings.NewReader(contentStr))
	if err != nil {
		return content // return original if parsing fails
	}

	var buf bytes.Buffer
	walkHTMLNode(&buf, doc, opts)
	return buf.Bytes()
}

// voidHTMLElements are elements that must not have closing tags.
var voidHTMLElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func walkHTMLNode(w io.Writer, n *html.Node, opts *htmlCleaningOptions) bool {
	if n.Type == html.ElementNode {
		if skipHTMLNode(n, opts) {
			return false
		}
		if !opts.KeepStyles {
			removeStyleAttribute(n)
		}
		if n.Data == "a" {
			sanitizeJSHref(n)
		}
	}

	if n.Type == html.ElementNode {
		io.WriteString(w, "<"+n.Data)
		for _, attr := range n.Attr {
			io.WriteString(w, " "+attr.Key+"=\""+html.EscapeString(attr.Val)+"\"")
		}
		if voidHTMLElements[n.Data] {
			io.WriteString(w, "/>")
			return true
		}
		io.WriteString(w, ">")
	} else if n.Type == html.TextNode {
		io.WriteString(w, html.EscapeString(n.Data))
	} else if n.Type == html.CommentNode && opts.KeepComments {
		io.WriteString(w, "<!--"+n.Data+"-->")
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTMLNode(w, c, opts)
	}

	if n.Type == html.ElementNode && !voidHTMLElements[n.Data] {
		io.WriteString(w, "</"+n.Data+">")
	}
	return true
}

func skipHTMLNode(n *html.Node, opts *htmlCleaningOptions) bool {
	switch n.Data {
	case "header":
		return !opts.KeepHeader
	case "footer":
		return !opts.KeepFooter
	case "nav":
		return !opts.KeepNav
	case "style":
		return !opts.KeepStyles
	}
	return false
}

func removeStyleAttribute(n *html.Node) {
	for i := 0; i < len(n.Attr); i++ {
		if n.Attr[i].Key == "style" {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			i--
		}
	}
}

func sanitizeJSHref(n *html.Node) {
	for i := range n.Attr {
		if n.Attr[i].Key == "href" &&
			strings.HasPrefix(strings.TrimSpace(strings.ToLower(n.Attr[i].Val)), "javascript:") {
			n.Attr[i].Val = "#"
		}
	}
}

// ─── Browser fetch with timeout ────────────────────────────────────────────

// fetchWithBrowser runs the browser fetch in a goroutine with a timeout.
func fetchWithBrowser(b fetchBrowser, url string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := b.Fetch(url)
		ch <- result{data, err}
	}()

	select {
	case r := <-ch:
		return r.data, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("browser fetch timed out after %v", timeout)
	}
}
