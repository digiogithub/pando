// Package systemone implements a client for the System One (Jev) decision
// protocol, `POST {baseURL}/v1/systemone`, served by Ollama >= 0.35, TypeSafe
// Jev and Jev-compatible gateways, plus the decision-provider layer
// (discovery, health, warm-up) built on top of it.
//
// The package deliberately does not depend on internal/config: callers pass
// plain Options.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// MaxBodyBytes is the request body cap enforced by Ollama (and locally).
	MaxBodyBytes = 64 * 1024
	// MaxQuestions is the maximum number of questions per request.
	MaxQuestions = 64
	// MinChoiceCriteria / MaxChoiceCriteria bound a choice question.
	MinChoiceCriteria = 2
	MaxChoiceCriteria = 26
	// DefaultTimeout is used when Options.Timeout is zero.
	DefaultTimeout = 30 * time.Second

	maxResponseBytes = 4 << 20
)

// Options configures a Client.
type Options struct {
	BaseURL string            // root URL, without /v1/systemone
	APIKey  string            // optional; sent as "Authorization: Bearer <key>"
	Headers map[string]string // extra headers sent on every request
	Timeout time.Duration     // per-call bound; DefaultTimeout when zero
	// HTTPClient overrides the transport (tests). Timeout is enforced through
	// the request context, not through HTTPClient.Timeout.
	HTTPClient *http.Client

	// Provider-level knobs (ignored by the raw client).
	KeepAlive     string // Ollama keep_alive for probes/warm-up; default "30m"
	ContextBudget int    // default context budget (tokens) for custom providers
}

// Criterion is one ordered entry of a choice question. A nil Description
// marshals as JSON null.
type Criterion struct {
	Key         string
	Description *string
}

// NewCriterion builds a Criterion; an empty description becomes null.
func NewCriterion(key, description string) Criterion {
	if description == "" {
		return Criterion{Key: key}
	}
	return Criterion{Key: key, Description: &description}
}

// Question is one System One question. Criteria is an ordered slice because
// the order of criteria is significant to the model; it marshals as a JSON
// object preserving insertion order.
type Question struct {
	Type         string // "choice", "noul" or "score"
	Instructions any    // string | object | array; required by the server
	Criteria     []Criterion
	// Extra holds additional type-specific fields merged into the question
	// object (e.g. score bounds).
	Extra map[string]any
}

// MarshalJSON emits {"type":..,"instructions":..,"criteria":{ordered}, ...extra}.
func (q Question) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	typ, _ := json.Marshal(q.Type)
	b.WriteString(`"type":`)
	b.Write(typ)
	if q.Instructions != nil {
		ins, err := json.Marshal(q.Instructions)
		if err != nil {
			return nil, err
		}
		b.WriteString(`,"instructions":`)
		b.Write(ins)
	}
	if len(q.Criteria) > 0 {
		b.WriteString(`,"criteria":{`)
		for i, c := range q.Criteria {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(c.Key)
			b.Write(k)
			b.WriteByte(':')
			if c.Description == nil {
				b.WriteString("null")
			} else {
				d, _ := json.Marshal(*c.Description)
				b.Write(d)
			}
		}
		b.WriteByte('}')
	}
	for k, v := range q.Extra {
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		b.WriteByte(',')
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Request is the System One request body.
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
	KeepAlive string              `json:"keep_alive,omitempty"`
}

// Answer is the decoded answer to one question.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Score         *float64           `json:"score,omitempty"`
}

// Usage reports token usage and, on gateways, the cost.
type Usage struct {
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

// Response is the System One response body.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Client talks to a System One endpoint. It is safe for concurrent use and
// never retries.
type Client struct {
	opts Options
	http *http.Client
}

// NewClient builds a Client.
func NewClient(o Options) *Client {
	o.BaseURL = strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{opts: o, http: hc}
}

// BaseURL returns the configured root URL.
func (c *Client) BaseURL() string { return c.opts.BaseURL }

// KeepAlive returns the configured Ollama keep_alive.
func (c *Client) KeepAlive() string { return c.opts.KeepAlive }

// Timeout returns the per-call bound.
func (c *Client) Timeout() time.Duration { return c.opts.Timeout }

// validate enforces the protocol bounds locally.
func (r *Request) validate() error {
	if r.Model == "" {
		return newAPIError(0, ErrBadRequest, "model is required")
	}
	n := len(r.Questions)
	if n < 1 || n > MaxQuestions {
		return newAPIError(0, ErrBadRequest, fmt.Sprintf("questions must contain 1-%d entries, got %d", MaxQuestions, n))
	}
	for name, q := range r.Questions {
		if q.Type == "choice" && (len(q.Criteria) < MinChoiceCriteria || len(q.Criteria) > MaxChoiceCriteria) {
			return newAPIError(0, ErrBadRequest, fmt.Sprintf("question %q: choice criteria must contain %d-%d candidates, got %d",
				name, MinChoiceCriteria, MaxChoiceCriteria, len(q.Criteria)))
		}
	}
	return nil
}

// Decide sends one System One request. No internal retry is performed.
func (c *Client) Decide(ctx context.Context, req Request) (*Response, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, newAPIError(0, ErrBadRequest, "cannot encode request: "+err.Error())
	}
	if len(body) > MaxBodyBytes {
		return nil, newAPIError(413, ErrTooLarge, fmt.Sprintf("request body is %d bytes, limit is %d", len(body), MaxBodyBytes))
	}

	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()

	data, status, err := c.do(ctx, http.MethodPost, "/v1/systemone", body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, c.statusError(status, data)
	}

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, newAPIError(status, ErrMalformedResponse, "invalid JSON: "+err.Error())
	}
	if err := checkAnswers(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func checkAnswers(req Request, resp *Response) error {
	for name, q := range req.Questions {
		a, ok := resp.Answers[name]
		if !ok {
			return newAPIError(200, ErrMalformedResponse, fmt.Sprintf("answer for question %q is missing", name))
		}
		if q.Type != "choice" {
			continue
		}
		found := false
		for _, c := range q.Criteria {
			if c.Key == a.Choice {
				found = true
				break
			}
		}
		if !found {
			return newAPIError(200, ErrMalformedResponse, fmt.Sprintf("question %q: choice %q is not among the criteria", name, a.Choice))
		}
	}
	return nil
}

// do performs one HTTP call and returns the body and status. Transport
// failures are mapped to ErrTimeout / ErrUnreachable.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.opts.BaseURL+path, rdr)
	if err != nil {
		return nil, 0, newAPIError(0, ErrBadRequest, c.redact(err.Error()))
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")
	for k, v := range c.opts.Headers {
		httpReq.Header.Set(k, v)
	}
	if c.opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, 0, c.transportError(ctx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, c.transportError(ctx, err)
	}
	return data, resp.StatusCode, nil
}

func (c *Client) transportError(ctx context.Context, err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return newAPIError(0, ErrTimeout, "deadline exceeded")
	case errors.Is(err, context.Canceled):
		return err
	default:
		return newAPIError(0, ErrUnreachable, c.redact(err.Error()))
	}
}

// statusError maps a non-2xx response to a typed error.
func (c *Client) statusError(status int, body []byte) error {
	return newAPIError(status, kindForStatus(status), c.redact(extractMessage(body)))
}

// redact removes the API key from text that may end up in error strings.
func (c *Client) redact(s string) string {
	if k := c.opts.APIKey; k != "" {
		s = strings.ReplaceAll(s, k, "***")
	}
	return s
}

func extractMessage(body []byte) string {
	var v struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil && len(v.Error) > 0 {
		var s string
		if json.Unmarshal(v.Error, &s) == nil {
			return s
		}
		var o struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(v.Error, &o) == nil && o.Message != "" {
			return o.Message
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// get performs a GET bounded by the client timeout; used by providers.
func (c *Client) get(ctx context.Context, path string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	return c.do(ctx, http.MethodGet, path, nil)
}

// postJSON performs a bounded JSON POST; used by providers.
func (c *Client) postJSON(ctx context.Context, path string, v any) ([]byte, int, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, 0, newAPIError(0, ErrBadRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	return c.do(ctx, http.MethodPost, path, b)
}
