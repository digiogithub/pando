package core

import "errors"

// ErrorCode enumerates the structured error codes surfaced to the LLM by
// the desktop tools. Every DesktopError carries exactly one of these.
type ErrorCode string

const (
	// ErrPermDenied signals the OS or the assistive-technology stack
	// denied access (e.g. accessibility permission not granted).
	ErrPermDenied ErrorCode = "PERM_DENIED"
	// ErrElementNotFound signals a selector or ref did not resolve to any
	// element.
	ErrElementNotFound ErrorCode = "ELEMENT_NOT_FOUND"
	// ErrAppNotFound signals the requested application/process was not
	// found among the running apps.
	ErrAppNotFound ErrorCode = "APP_NOT_FOUND"
	// ErrStaleRef signals a qualified ElementRef's snapshot expired or was
	// evicted, or the element no longer exists within it.
	ErrStaleRef ErrorCode = "STALE_REF"
	// ErrSnapshotNotFound signals a snapshot id is unknown to the store.
	ErrSnapshotNotFound ErrorCode = "SNAPSHOT_NOT_FOUND"
	// ErrPolicyDenied signals the action was blocked by the allow/deny
	// application policy.
	ErrPolicyDenied ErrorCode = "POLICY_DENIED"
	// ErrActionFailed signals a backend action (native or physical) was
	// attempted and failed.
	ErrActionFailed ErrorCode = "ACTION_FAILED"
	// ErrPlatformNotSupported signals the current platform/backend cannot
	// perform the requested operation.
	ErrPlatformNotSupported ErrorCode = "PLATFORM_NOT_SUPPORTED"
	// ErrTimeout signals a wait/retry loop exceeded its deadline.
	ErrTimeout ErrorCode = "TIMEOUT"
	// ErrInvalidArgs signals malformed input, e.g. an unparsable selector
	// or ref.
	ErrInvalidArgs ErrorCode = "INVALID_ARGS"
)

// defaultSuggestions maps each ErrorCode to an actionable default hint.
var defaultSuggestions = map[ErrorCode]string{
	ErrPermDenied:           "Grant accessibility/automation permission to Pando and retry.",
	ErrElementNotFound:      "Re-observe the app/window and adjust the selector; the element may not exist yet.",
	ErrAppNotFound:          "List running applications with desktop_apps and use an exact app id/name.",
	ErrStaleRef:             "The snapshot expired or was evicted; take a fresh desktop_observe/desktop_find and use the new ref.",
	ErrSnapshotNotFound:     "The snapshot id is unknown; take a fresh desktop_observe/desktop_find.",
	ErrPolicyDenied:         "This application is blocked by the desktop policy; ask the user to adjust the allow/deny list.",
	ErrActionFailed:         "The action was attempted but failed; retry, or fall back to a different action/selector.",
	ErrPlatformNotSupported: "This capability is not available on the current platform/backend.",
	ErrTimeout:              "The condition did not become true within the timeout; retry with a longer timeout or verify the selector.",
	ErrInvalidArgs:          "Fix the malformed argument and retry.",
}

// DesktopError is the structured error type returned by every uiauto
// operation that can fail in an LLM-actionable way.
type DesktopError struct {
	Code       ErrorCode
	Message    string
	Suggestion string
}

// Error implements the error interface.
func (e *DesktopError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

// newDesktopError builds a DesktopError, using the default suggestion for
// code when suggestion is empty.
func newDesktopError(code ErrorCode, message, suggestion string) *DesktopError {
	if suggestion == "" {
		suggestion = defaultSuggestions[code]
	}
	return &DesktopError{Code: code, Message: message, Suggestion: suggestion}
}

// NewPermDeniedError constructs a PERM_DENIED DesktopError.
func NewPermDeniedError(message string) *DesktopError {
	return newDesktopError(ErrPermDenied, message, "")
}

// NewElementNotFoundError constructs an ELEMENT_NOT_FOUND DesktopError.
func NewElementNotFoundError(message string) *DesktopError {
	return newDesktopError(ErrElementNotFound, message, "")
}

// NewAppNotFoundError constructs an APP_NOT_FOUND DesktopError.
func NewAppNotFoundError(message string) *DesktopError {
	return newDesktopError(ErrAppNotFound, message, "")
}

// NewStaleRefError constructs a STALE_REF DesktopError.
func NewStaleRefError(message string) *DesktopError {
	return newDesktopError(ErrStaleRef, message, "")
}

// NewSnapshotNotFoundError constructs a SNAPSHOT_NOT_FOUND DesktopError.
func NewSnapshotNotFoundError(message string) *DesktopError {
	return newDesktopError(ErrSnapshotNotFound, message, "")
}

// NewPolicyDeniedError constructs a POLICY_DENIED DesktopError.
func NewPolicyDeniedError(message string) *DesktopError {
	return newDesktopError(ErrPolicyDenied, message, "")
}

// NewActionFailedError constructs an ACTION_FAILED DesktopError.
func NewActionFailedError(message string) *DesktopError {
	return newDesktopError(ErrActionFailed, message, "")
}

// NewPlatformNotSupportedError constructs a PLATFORM_NOT_SUPPORTED
// DesktopError.
func NewPlatformNotSupportedError(message string) *DesktopError {
	return newDesktopError(ErrPlatformNotSupported, message, "")
}

// NewTimeoutError constructs a TIMEOUT DesktopError.
func NewTimeoutError(message string) *DesktopError {
	return newDesktopError(ErrTimeout, message, "")
}

// NewInvalidArgsError constructs an INVALID_ARGS DesktopError.
func NewInvalidArgsError(message string) *DesktopError {
	return newDesktopError(ErrInvalidArgs, message, "")
}

// AsDesktopError unwraps err into a *DesktopError if it is (or wraps) one.
func AsDesktopError(err error) (*DesktopError, bool) {
	if err == nil {
		return nil, false
	}
	var de *DesktopError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// Payload renders the DesktopError as the structured, LLM-facing response
// shape: {"ok":false,"error":{"code":...,"message":...,"suggestion":...}}.
func (e *DesktopError) Payload() map[string]any {
	if e == nil {
		return map[string]any{"ok": true}
	}
	return map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":       string(e.Code),
			"message":    e.Message,
			"suggestion": e.Suggestion,
		},
	}
}
