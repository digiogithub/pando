package systemone

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned (wrapped in *APIError) by the System One client.
// Use errors.Is to classify a failure.
var (
	// ErrUnreachable means the backend could not be contacted (connection refused, DNS, ...).
	ErrUnreachable = errors.New("systemone: backend unreachable")
	// ErrUnauthorized maps HTTP 401/403.
	ErrUnauthorized = errors.New("systemone: unauthorized")
	// ErrModelNotFound maps HTTP 404.
	ErrModelNotFound = errors.New("systemone: model not found")
	// ErrBadRequest maps HTTP 400 (including Ollama ":cloud" models).
	ErrBadRequest = errors.New("systemone: bad request")
	// ErrTooLarge maps HTTP 413 and the local 64 KiB body cap.
	ErrTooLarge = errors.New("systemone: request too large")
	// ErrRateLimited maps HTTP 429.
	ErrRateLimited = errors.New("systemone: rate limited")
	// ErrServer maps HTTP 5xx.
	ErrServer = errors.New("systemone: server error")
	// ErrTimeout means the per-call deadline elapsed.
	ErrTimeout = errors.New("systemone: timeout")
	// ErrMalformedResponse means the body was not valid JSON, an answer was
	// missing, or a choice is not among the question criteria.
	ErrMalformedResponse = errors.New("systemone: malformed response")

	// ErrUnavailable is a convenience class: errors.Is(err, ErrUnavailable) is
	// true for ErrUnreachable and ErrServer.
	ErrUnavailable = errors.New("systemone: backend unavailable")
	// ErrMalformed is an alias of ErrMalformedResponse.
	ErrMalformed = ErrMalformedResponse
)

// APIError is the concrete error type returned by the client. Status is the
// HTTP status (0 when no response was received), Kind one of the sentinels.
type APIError struct {
	Status  int
	Message string
	Kind    error
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString(e.Kind.Error())
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Unwrap exposes the sentinel kind to errors.Is / errors.As.
func (e *APIError) Unwrap() error { return e.Kind }

// Is additionally matches the ErrUnavailable class.
func (e *APIError) Is(target error) bool {
	if target == ErrUnavailable {
		return e.Kind == ErrUnreachable || e.Kind == ErrServer
	}
	return false
}

func newAPIError(status int, kind error, msg string) *APIError {
	return &APIError{Status: status, Kind: kind, Message: msg}
}

func kindForStatus(status int) error {
	switch {
	case status == 400:
		return ErrBadRequest
	case status == 401 || status == 403:
		return ErrUnauthorized
	case status == 404:
		return ErrModelNotFound
	case status == 413:
		return ErrTooLarge
	case status == 429:
		return ErrRateLimited
	case status >= 500:
		return ErrServer
	default:
		return ErrBadRequest
	}
}
