package dbproxy

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsMethodNotFound(t *testing.T) {
	structured := fmt.Errorf("wrap: %w", &WriteError{Code: ErrCodeMethodNotFound, Method: "X"})
	flattened := errors.New(`dbproxy: INTERNAL (ReplaceSessionEvents): ipc: RPC error -32000: dbproxy: METHOD_NOT_FOUND (ReplaceSessionEvents): unknown write method "ReplaceSessionEvents"`)
	other := &WriteError{Code: ErrCodeTimeout, Method: "X"}

	if !IsMethodNotFound(structured) {
		t.Error("structured METHOD_NOT_FOUND not detected")
	}
	if !IsMethodNotFound(flattened) {
		t.Error("METHOD_NOT_FOUND flattened over IPC not detected")
	}
	if IsMethodNotFound(other) || IsMethodNotFound(nil) {
		t.Error("false positive")
	}
}
