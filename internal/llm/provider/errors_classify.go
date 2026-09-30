package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

// ErrorClass is a coarse, provider-agnostic category for an LLM provider
// error. It drives Auto-mode provider failover (PANDO-SP-0003.R6).
type ErrorClass string

const (
	ErrorClassNone          ErrorClass = ""
	ErrorClassCanceled      ErrorClass = "canceled"       // ctx canceled by user: never failover
	ErrorClassContextLength ErrorClass = "context_length" // prompt too long: no failover (compaction path)
	ErrorClassContentPolicy ErrorClass = "content_policy" // safety refusal: no failover
	ErrorClassAuth          ErrorClass = "auth"           // 401/403, invalid key: failover + cooldown
	ErrorClassNotFound      ErrorClass = "not_found"      // model 404: failover + cooldown
	ErrorClassRateLimit     ErrorClass = "rate_limit"     // 429 after retries: failover
	ErrorClassServer        ErrorClass = "server"         // 5xx/overloaded: failover
	ErrorClassNetwork       ErrorClass = "network"        // dial/timeout/EOF: failover
	ErrorClassBadRequest    ErrorClass = "bad_request"    // other 400: failover
	ErrorClassTool          ErrorClass = "tool"           // tool execution error: never failover
	ErrorClassUnknown       ErrorClass = "unknown"        // failover
)

// ShouldFailover reports whether an error of this class justifies switching
// the turn to the next candidate model.
func (c ErrorClass) ShouldFailover() bool {
	switch c {
	case ErrorClassAuth, ErrorClassNotFound, ErrorClassRateLimit, ErrorClassServer,
		ErrorClassNetwork, ErrorClassBadRequest, ErrorClassUnknown:
		return true
	}
	return false
}

// NeedsCooldown reports whether a candidate failing with this class should be
// skipped by later turns for a cooldown period.
func (c ErrorClass) NeedsCooldown() bool {
	return c == ErrorClassAuth || c == ErrorClassNotFound
}

// String implements fmt.Stringer.
func (c ErrorClass) String() string { return string(c) }

// toolError marks an error raised by tool execution.
type toolError struct{ err error }

func (e *toolError) Error() string { return e.err.Error() }
func (e *toolError) Unwrap() error { return e.err }

// MarkToolError wraps err so ClassifyError reports ErrorClassTool. A nil err
// yields nil.
func MarkToolError(err error) error {
	if err == nil {
		return nil
	}
	return &toolError{err: err}
}

var (
	statusInTextRe = regexp.MustCompile(`(?i)(?:status(?:\s*code)?|error|http|response|code)[\s:=]+(\d{3})\b`)
	statusAfterRe  = regexp.MustCompile(`:\s(\d{3})\s[A-Za-z]`)
)

// ClassifyError maps err to an ErrorClass. It understands typed SDK errors
// (openai-go, anthropic-sdk-go, genai), wrapped errors, network errors and
// falls back to status codes and well-known phrases in the message.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorClassNone
	}

	var te *toolError
	if errors.As(err, &te) {
		return ErrorClassTool
	}
	if errors.Is(err, context.Canceled) {
		return ErrorClassCanceled
	}

	status, text := extractStatusAndText(err)
	lower := strings.ToLower(text)

	if lower == "" {
		lower = strings.ToLower(err.Error())
	}
	if strings.Contains(lower, "request cancelled by user") || strings.Contains(lower, "context canceled") {
		return ErrorClassCanceled
	}

	if status == 413 || containsAny(lower, contextLengthPhrases) {
		return ErrorClassContextLength
	}
	if containsAny(lower, contentPolicyPhrases) {
		return ErrorClassContentPolicy
	}

	// Status codes that are unambiguous win over message phrases.
	switch {
	case status == 401 || status == 403:
		return ErrorClassAuth
	case status == 404:
		return ErrorClassNotFound
	case status == 408:
		return ErrorClassNetwork
	case status == 429:
		return ErrorClassRateLimit
	case status >= 500 && status <= 599:
		return ErrorClassServer
	}

	if isNetworkError(err) {
		return ErrorClassNetwork
	}
	if trimmed := strings.TrimSpace(lower); trimmed == "eof" || strings.HasSuffix(trimmed, ": eof") {
		return ErrorClassNetwork
	}
	if c := classifyPhrases(lower); c != ErrorClassNone {
		return c
	}
	if status >= 400 && status <= 499 {
		return ErrorClassBadRequest
	}
	return ErrorClassUnknown
}

// extractStatusAndText pulls an HTTP status and a searchable message out of
// typed SDK errors, falling back to parsing the error string.
func extractStatusAndText(err error) (int, string) {
	var oe *openai.Error
	if errors.As(err, &oe) && oe != nil {
		return oe.StatusCode, safeErrorString(oe) + " " + oe.Code + " " + oe.Type + " " + oe.Message
	}
	var ae *anthropic.Error
	if errors.As(err, &ae) && ae != nil {
		return ae.StatusCode, safeErrorString(ae) + " " + ae.RawJSON() + " " + err.Error()
	}
	var ge genai.APIError
	if errors.As(err, &ge) {
		return ge.Code, err.Error() + " " + ge.Status + " " + ge.Message
	}
	var gp *genai.APIError
	if errors.As(err, &gp) && gp != nil {
		return gp.Code, err.Error() + " " + gp.Status + " " + gp.Message
	}

	msg := err.Error()
	if m := statusInTextRe.FindStringSubmatch(msg); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 400 && n <= 599 {
			return n, msg
		}
	}
	if m := statusAfterRe.FindStringSubmatch(msg); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 400 && n <= 599 {
			return n, msg
		}
	}
	return 0, msg
}

// safeErrorString guards against SDK Error() methods that dereference nil
// Request/Response fields.
func safeErrorString(err error) (s string) {
	defer func() {
		if recover() != nil {
			s = ""
		}
	}()
	return err.Error()
}

func isNetworkError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var dns *net.DNSError
	var op *net.OpError
	var rh tls.RecordHeaderError
	var ua x509.UnknownAuthorityError
	var ci x509.CertificateInvalidError
	var hn x509.HostnameError
	return errors.As(err, &dns) || errors.As(err, &op) || errors.As(err, &rh) ||
		errors.As(err, &ua) || errors.As(err, &ci) || errors.As(err, &hn)
}

var contextLengthPhrases = []string{
	"context_length_exceeded",
	"maximum context length",
	"prompt is too long",
	"prompt too long",
	"context window",
	"context length",
	"exceed context limit",
	"exceeds the maximum number of tokens",
	"exceeds the model's token limit",
	"input is too long",
	"input token count",
	"request too large",
	"too many tokens",
	"reduce the length of the messages",
	"string_above_max_length",
}

var contentPolicyPhrases = []string{
	"content_filter",
	"content filter",
	"content_policy",
	"content policy",
	"responsibleaipolicyviolation",
	"blocked due to safety",
	"blocked by safety",
	"finish reason: safety",
	"finishreason: safety",
	"prohibited_content",
	"violates our usage policies",
	"stop_reason\":\"refusal",
	"stop_reason: refusal",
}

func classifyPhrases(lower string) ErrorClass {
	switch {
	case containsAny(lower, []string{
		"invalid api key", "invalid_api_key", "incorrect api key", "api key not valid",
		"api key is invalid", "unauthorized", "unauthenticated", "permission denied",
		"permission_denied", "authentication_error", "authentication failed",
		"accessdeniedexception", "forbidden", "invalid x-api-key", "expired token",
		"unrecognizedclientexception", "bad credentials",
	}):
		return ErrorClassAuth
	case containsAny(lower, []string{
		"model_not_found", "model not found", "resourcenotfoundexception",
		"not_found_error", "does not exist", "unknown model", "model_not_available",
		"is not found for api version", "no such model",
	}) || (strings.Contains(lower, "model") && strings.Contains(lower, "not found")):
		return ErrorClassNotFound
	case containsAny(lower, []string{
		"rate limit", "rate_limit", "ratelimit", "quota", "too many requests",
		"resource_exhausted", "throttlingexception", "throttled", "usage limit",
		"tokens per minute", "requests per minute", "limit reached after",
	}):
		return ErrorClassRateLimit
	case containsAny(lower, []string{
		"overloaded", "overloaded_error", "service unavailable", "serviceunavailableexception",
		"internal server error", "internal_server_error", "bad gateway", "gateway timeout",
		"server_error", "modelnotreadyexception", "modeltimeoutexception",
		"temporarily unavailable", "unavailable",
	}):
		return ErrorClassServer
	case containsAny(lower, []string{
		"connection refused", "connection reset", "no such host", "i/o timeout",
		"unexpected eof", "broken pipe", "tls:", "x509:", "dial tcp", "network is unreachable",
		"no route to host", "timeout", "timed out", "deadline exceeded",
		"server closed idle connection", "stream error", "http2:",
	}):
		return ErrorClassNetwork
	case containsAny(lower, []string{"invalid_request_error", "invalid_argument", "validationexception", "bad request"}):
		return ErrorClassBadRequest
	}
	return ErrorClassNone
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
