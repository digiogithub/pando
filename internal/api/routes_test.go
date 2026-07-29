package api

import (
	"net/http"
	"testing"
)

// TestRegisterRoutesNoPatternConflict guards against http.ServeMux pattern
// conflicts, which panic at registration time and therefore crash every command
// that builds an API server (`pando app`, `pando serve`, ...) before it can
// print anything useful.
func TestRegisterRoutesNoPatternConflict(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerRoutes panicked: %v", r)
		}
	}()

	(&Server{}).registerRoutes(http.NewServeMux())
}
