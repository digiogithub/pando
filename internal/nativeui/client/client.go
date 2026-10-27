// Package client is the Go client of the Pando HTTP API used by the native
// desktop UI. It speaks the same contract as the React Web UI
// (web-ui/packages/pando-client) and ports the protocol lessons of the Rust
// GPUI client (pando-assistant/crates/pando-client): token acquisition and
// refresh, the chat SSE stream with run reattach, steering, and the global
// event streams.
//
// The package must not import MyGo: it is UI-toolkit agnostic so it can be
// unit tested with httptest and reused by any front end.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Header names and values of the Pando API.
const (
	HeaderToken  = "X-Pando-Token"
	HeaderClient = "X-Pando-Client"
	// ClientName identifies the native desktop to the server.
	ClientName = "desktop"
	tokenPath  = "/api/v1/token"
)

// ErrUnauthorized is returned when the server rejects the token even after a
// refresh.
var ErrUnauthorized = errors.New("pando: unauthorized")

// ErrBasicAuthRequired is returned when an exposed server demands basic-auth
// credentials before it hands out a token.
var ErrBasicAuthRequired = errors.New("pando: basic auth required")

// APIError is a non-2xx answer of the API.
type APIError struct {
	Status int
	// Code is the `error` field of a JSON error body, when present.
	Code string
	Body string
}

func (e *APIError) Error() string {
	msg := e.Code
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("pando: HTTP %d: %s", e.Status, msg)
}

// IsStatus reports whether err is an APIError with the given status.
func IsStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status
}

// Options configures a Client.
type Options struct {
	// BaseURL of the server, e.g. http://localhost:8765. A path prefix
	// (project child mode) is kept.
	BaseURL string
	// Token is the API token, when already known (in-process server). Left
	// empty, it is fetched from GET /api/v1/token on first use.
	Token string
	// CAFile is a PEM bundle trusted in addition to the system roots for
	// https servers. Empty uses DefaultCAFile() when it exists.
	CAFile string
	// BasicAuthUser and BasicAuthPassword are sent to /api/v1/token on
	// exposed servers that enforce basic auth.
	BasicAuthUser, BasicAuthPassword string
	// HTTPClient overrides the transport (tests). Streams use it with no
	// timeout; REST calls are bounded by RequestTimeout.
	HTTPClient *http.Client
	// RequestTimeout bounds REST calls (default 30s). Streams are bounded
	// only by their context.
	RequestTimeout time.Duration
}

// Client is a Pando API client. It is safe for concurrent use.
type Client struct {
	base    *url.URL
	http    *http.Client
	timeout time.Duration
	user    string
	pass    string

	// refreshMu serializes token fetches, so concurrent 401s refresh once.
	refreshMu sync.Mutex
	mu        sync.Mutex
	token     string
	// parent, when set, owns the token (see Derive).
	parent *Client
	// tokenGen counts token refreshes, so concurrent 401s refresh once.
	tokenGen int
}

// DefaultCAFile is Pando's local CA (internal/tlsutil), trusted so a local
// https server works without importing it into the system store.
func DefaultCAFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pando", "tls", "ca.crt")
}

// New returns a client for opts.BaseURL.
func New(opts Options) (*Client, error) {
	raw := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if raw == "" {
		return nil, errors.New("pando: empty base URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("pando: invalid base URL %q", opts.BaseURL)
	}
	hc := opts.HTTPClient
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if u.Scheme == "https" {
			cfg, err := tlsConfig(opts.CAFile)
			if err != nil {
				return nil, err
			}
			tr.TLSClientConfig = cfg
		}
		hc = &http.Client{Transport: tr}
	}
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		base:    u,
		http:    hc,
		timeout: timeout,
		token:   opts.Token,
		user:    opts.BasicAuthUser,
		pass:    opts.BasicAuthPassword,
	}, nil
}

func tlsConfig(caFile string) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	explicit := caFile != ""
	if !explicit {
		caFile = DefaultCAFile()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		switch {
		case err == nil:
			if !pool.AppendCertsFromPEM(pem) && explicit {
				return nil, fmt.Errorf("pando: no certificate in %s", caFile)
			}
		case explicit:
			return nil, fmt.Errorf("pando: read CA: %w", err)
		}
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// BaseURL returns the server base URL.
func (c *Client) BaseURL() string { return c.base.String() }

// URL resolves an API path against the base URL.
func (c *Client) URL(path string) string {
	if strings.Contains(path, "://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(c.base.String(), "/") + path
}

// Token returns the API token, fetching it on first use.
func (c *Client) Token(ctx context.Context) (string, error) {
	if c.parent != nil {
		return c.parent.Token(ctx)
	}
	c.mu.Lock()
	tok, gen := c.token, c.tokenGen
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	return c.refreshToken(ctx, gen)
}

// refreshToken fetches a new token unless another goroutine already did
// since generation gen was observed.
func (c *Client) refreshToken(ctx context.Context, gen int) (string, error) {
	if c.parent != nil {
		return c.parent.refreshToken(ctx, gen)
	}
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.mu.Lock()
	if c.tokenGen != gen && c.token != "" {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	old := c.token
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL(tokenPath), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(HeaderClient, ClientName)
	if old != "" {
		// An exposed server without basic auth hands a new token only to a
		// caller holding the current one.
		req.Header.Set(HeaderToken, old)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("pando: fetch token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		if bytes.Contains(body, []byte("basic_auth")) || resp.Header.Get("WWW-Authenticate") != "" {
			return "", ErrBasicAuthRequired
		}
		return "", ErrUnauthorized
	}
	if resp.StatusCode/100 != 2 {
		return "", apiError(resp.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" {
		return "", errors.New("pando: token endpoint returned no token")
	}
	c.mu.Lock()
	c.token = out.Token
	c.tokenGen++
	c.mu.Unlock()
	return out.Token, nil
}

func apiError(status int, body []byte) error {
	e := &APIError{Status: status, Body: string(body)}
	var j struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &j) == nil {
		e.Code = j.Error
	}
	return e
}

// send performs an authenticated request, refreshing the token once on 401.
// body is JSON-encoded unless it is nil. The caller closes the response
// body of a 2xx answer; other answers come back as *APIError.
func (c *Client) send(ctx context.Context, method, path string, body any, stream bool) (*http.Response, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}
	for attempt := 0; ; attempt++ {
		tokOwner := c
		if c.parent != nil {
			tokOwner = c.parent
		}
		tokOwner.mu.Lock()
		gen := tokOwner.tokenGen
		tokOwner.mu.Unlock()
		tok, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.URL(path), rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set(HeaderToken, tok)
		req.Header.Set(HeaderClient, ClientName)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		} else {
			req.Header.Set("Accept", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			drain(resp)
			// The token is stale (regenerated, or the server restarted):
			// fetch a new one, unless a concurrent call already did.
			if _, err := c.refreshToken(ctx, gen); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return nil, ErrUnauthorized
			}
			return nil, apiError(resp.StatusCode, b)
		}
		return resp, nil
	}
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}

// Do performs a JSON REST call: body (nil for none) is encoded as JSON, the
// answer decoded into out (nil to discard). It is the building block for
// views that need endpoints this package does not wrap yet.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.send(ctx, method, path, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if s, ok := out.(*string); ok && !json.Valid(data) {
		*s = string(data)
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("pando: decode %s %s: %w", method, path, err)
	}
	return nil
}

// Get is Do with GET.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post is Do with POST.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, body, out)
}

// Put is Do with PUT.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPut, path, body, out)
}

// Patch is Do with PATCH.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPatch, path, body, out)
}

// Delete is Do with DELETE.
func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodDelete, path, nil, out)
}

// Health is GET /health (no token needed).
func (c *Client) Health(ctx context.Context) (ServerInfo, error) {
	var info ServerInfo
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL("/health"), nil)
	if err != nil {
		return info, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return info, apiError(resp.StatusCode, body)
	}
	err = json.Unmarshal(body, &info)
	return info, err
}

// OpenStream opens an authenticated SSE stream and calls fn for every frame
// until the body ends (nil), ctx is done, or fn returns an error. method is
// GET or POST (body JSON-encoded).
func (c *Client) OpenStream(ctx context.Context, method, path string, body any, fn func(SSEFrame) error) error {
	resp, err := c.send(ctx, method, path, body, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Close the body when ctx ends so a blocked Read returns.
	stop := context.AfterFunc(ctx, func() { resp.Body.Close() })
	defer stop()
	return readSSE(ctx, resp.Body, fn)
}

func pathEscape(id string) string { return url.PathEscape(id) }
