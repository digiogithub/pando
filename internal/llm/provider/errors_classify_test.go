package provider

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

func newOpenAIErr(t *testing.T, status int, body string) error {
	t.Helper()
	e := &openai.Error{}
	if err := json.Unmarshal([]byte(body), e); err != nil {
		t.Fatalf("unmarshal openai error: %v", err)
	}
	e.StatusCode = status
	e.Request = &http.Request{Method: "POST", URL: &url.URL{Scheme: "https", Host: "api.openai.com", Path: "/v1/chat/completions"}}
	e.Response = &http.Response{StatusCode: status, Header: http.Header{}}
	return e
}

func newAnthropicErr(t *testing.T, status int, body string) error {
	t.Helper()
	e := &anthropic.Error{}
	if err := json.Unmarshal([]byte(body), e); err != nil {
		t.Fatalf("unmarshal anthropic error: %v", err)
	}
	e.StatusCode = status
	e.Request = &http.Request{Method: "POST", URL: &url.URL{Scheme: "https", Host: "api.anthropic.com", Path: "/v1/messages"}}
	e.Response = &http.Response{StatusCode: status, Header: http.Header{}}
	return e
}

func TestClassifyProviderErrors(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("provider call failed: %w", err) }

	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{"nil", nil, ErrorClassNone},

		// canceled
		{"ctx canceled", context.Canceled, ErrorClassCanceled},
		{"ctx canceled wrapped", wrap(context.Canceled), ErrorClassCanceled},
		{"agent cancelled message", errors.New("request cancelled by user"), ErrorClassCanceled},

		// tool
		{"tool error", MarkToolError(errors.New("exit status 1")), ErrorClassTool},
		{"tool error wrapping 429 text", wrap(MarkToolError(errors.New("http 429 from fetched page"))), ErrorClassTool},

		// context length
		{"openai context_length_exceeded", newOpenAIErr(t, 400, `{"code":"context_length_exceeded","message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens.","param":"messages","type":"invalid_request_error"}`), ErrorClassContextLength},
		{"openai 413", newOpenAIErr(t, 413, `{"code":"","message":"Payload too large","param":"","type":""}`), ErrorClassContextLength},
		{"openai 413 pando wrapper", errors.New("request too large: the conversation context exceeds the model's token limit. Details: x"), ErrorClassContextLength},
		{"anthropic prompt too long", newAnthropicErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 215000 tokens > 200000 maximum"}}`), ErrorClassContextLength},
		{"anthropic max_tokens overflow", newAnthropicErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"input length and `+"`max_tokens`"+` exceed context limit: 190000 + 32000 > 200000"}}`), ErrorClassContextLength},
		{"gemini input token count", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT", Message: "The input token count (1200000) exceeds the maximum number of tokens allowed (1048576)."}, ErrorClassContextLength},
		{"ollama context length", errors.New("400 Bad Request: prompt too long; exceeded max context length by 1234 tokens"), ErrorClassContextLength},
		{"bedrock input too long", errors.New("ValidationException: Input is too long for requested model"), ErrorClassContextLength},

		// content policy
		{"openai content_filter", newOpenAIErr(t, 400, `{"code":"content_filter","message":"The response was filtered due to the prompt triggering content management policy.","param":"prompt","type":"invalid_request_error"}`), ErrorClassContentPolicy},
		{"azure content policy", errors.New("RESPONSE 400: 400 Bad Request ERROR CODE: content_policy_violation"), ErrorClassContentPolicy},
		{"gemini safety", errors.New("response blocked due to safety settings"), ErrorClassContentPolicy},
		{"anthropic refusal", errors.New("model finished with stop_reason: refusal"), ErrorClassContentPolicy},

		// auth
		{"openai 401", newOpenAIErr(t, 401, `{"code":"invalid_api_key","message":"Incorrect API key provided: sk-***","param":"","type":"invalid_request_error"}`), ErrorClassAuth},
		{"anthropic 401", newAnthropicErr(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`), ErrorClassAuth},
		{"anthropic 403", newAnthropicErr(t, 403, `{"type":"error","error":{"type":"permission_error","message":"Your credit balance is too low"}}`), ErrorClassAuth},
		{"gemini 403", genai.APIError{Code: 403, Status: "PERMISSION_DENIED", Message: "Permission denied"}, ErrorClassAuth},
		{"gemini key invalid 400", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT", Message: "API key not valid. Please pass a valid API key."}, ErrorClassAuth},
		{"copilot 401 wrapped", wrap(newOpenAIErr(t, 401, `{"code":"","message":"unauthorized: token expired","param":"","type":""}`)), ErrorClassAuth},
		{"bedrock access denied", errors.New("operation error Bedrock Runtime: Converse, https response error StatusCode: 403, AccessDeniedException: You don't have access"), ErrorClassAuth},
		{"vertex pointer gemini 401", &genai.APIError{Code: 401, Status: "UNAUTHENTICATED", Message: "Request had invalid authentication credentials"}, ErrorClassAuth},

		// not found
		{"openai model 404", newOpenAIErr(t, 404, `{"code":"model_not_found","message":"The model gpt-9 does not exist","param":"","type":"invalid_request_error"}`), ErrorClassNotFound},
		{"anthropic 404", newAnthropicErr(t, 404, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-x"}}`), ErrorClassNotFound},
		{"gemini 404", genai.APIError{Code: 404, Status: "NOT_FOUND", Message: "models/gemini-x is not found for API version v1beta"}, ErrorClassNotFound},
		{"ollama model not found text", errors.New("model 'tev1:9b' not found"), ErrorClassNotFound},
		{"bedrock resource not found", errors.New("ResourceNotFoundException: Could not resolve the foundation model"), ErrorClassNotFound},

		// rate limit
		{"openai 429", newOpenAIErr(t, 429, `{"code":"rate_limit_exceeded","message":"Rate limit reached for gpt-4o","param":"","type":"requests"}`), ErrorClassRateLimit},
		{"anthropic 429", newAnthropicErr(t, 429, `{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your rate limit"}}`), ErrorClassRateLimit},
		{"gemini 429", genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "Quota exceeded"}, ErrorClassRateLimit},
		{"openai retries exhausted", errors.New("maximum retry attempts reached for rate limit: 10 retries"), ErrorClassRateLimit},
		{"anthropic weekly limit", errors.New("Weekly limit reached after 1 attempts"), ErrorClassRateLimit},
		{"bedrock throttling", errors.New("ThrottlingException: Too many requests, please wait before trying again"), ErrorClassRateLimit},

		// server
		{"openai 500", newOpenAIErr(t, 500, `{"code":"","message":"The server had an error processing your request","param":"","type":"server_error"}`), ErrorClassServer},
		{"openai 503", newOpenAIErr(t, 503, `{"code":"","message":"unavailable","param":"","type":""}`), ErrorClassServer},
		{"anthropic 529", newAnthropicErr(t, 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), ErrorClassServer},
		{"anthropic overloaded text", errors.New("overloaded_error: Overloaded"), ErrorClassServer},
		{"gemini 503", genai.APIError{Code: 503, Status: "UNAVAILABLE", Message: "The model is overloaded. Please try again later."}, ErrorClassServer},
		{"text status 502", errors.New("unexpected response: status 502 from upstream"), ErrorClassServer},
		{"bedrock service unavailable", errors.New("ServiceUnavailableException: Bedrock is unable to process your request"), ErrorClassServer},

		// network
		{"io.EOF", io.EOF, ErrorClassNetwork},
		{"unexpected EOF wrapped", wrap(io.ErrUnexpectedEOF), ErrorClassNetwork},
		{"deadline exceeded", context.DeadlineExceeded, ErrorClassNetwork},
		{"conn refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, ErrorClassNetwork},
		{"conn reset wrapped", wrap(syscall.ECONNRESET), ErrorClassNetwork},
		{"dns", &net.DNSError{Err: "no such host", Name: "api.example.com"}, ErrorClassNetwork},
		{"tls unknown authority", x509.UnknownAuthorityError{}, ErrorClassNetwork},
		{"text i/o timeout", errors.New("Post \"https://x/v1\": dial tcp 1.2.3.4:443: i/o timeout"), ErrorClassNetwork},
		{"text plain EOF", errors.New("EOF"), ErrorClassNetwork},
		{"status 408", newOpenAIErr(t, 408, `{"code":"","message":"timeout","param":"","type":""}`), ErrorClassNetwork},

		// bad request
		{"openai 400 other", newOpenAIErr(t, 400, `{"code":"","message":"Invalid parameter: messages with role 'tool' must follow a tool_calls message","param":"messages","type":"invalid_request_error"}`), ErrorClassBadRequest},
		{"anthropic 400 other", newAnthropicErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1: tool_use ids were found without tool_result blocks"}}`), ErrorClassBadRequest},
		{"gemini 400 other", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT", Message: "Request contains an invalid argument."}, ErrorClassBadRequest},
		{"openai 422", newOpenAIErr(t, 422, `{"code":"","message":"unprocessable","param":"","type":""}`), ErrorClassBadRequest},

		// unknown
		{"unknown plain", errors.New("something odd happened"), ErrorClassUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Fatalf("ClassifyError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestErrorClassPolicy(t *testing.T) {
	failover := map[ErrorClass]bool{
		ErrorClassNone: false, ErrorClassCanceled: false, ErrorClassContextLength: false,
		ErrorClassContentPolicy: false, ErrorClassTool: false,
		ErrorClassAuth: true, ErrorClassNotFound: true, ErrorClassRateLimit: true,
		ErrorClassServer: true, ErrorClassNetwork: true, ErrorClassBadRequest: true,
		ErrorClassUnknown: true,
	}
	for c, want := range failover {
		if got := c.ShouldFailover(); got != want {
			t.Errorf("%q.ShouldFailover() = %v, want %v", c, got, want)
		}
		wantCool := c == ErrorClassAuth || c == ErrorClassNotFound
		if got := c.NeedsCooldown(); got != wantCool {
			t.Errorf("%q.NeedsCooldown() = %v, want %v", c, got, wantCool)
		}
	}
}
