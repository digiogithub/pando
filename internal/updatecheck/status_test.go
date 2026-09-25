package updatecheck

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/version"
)

func TestCurrentStatusDevBuildSkipsLookup(t *testing.T) {
	prev := version.Version
	version.Version = "unknown"
	t.Cleanup(func() { version.Version = prev })

	got := CurrentStatus(context.Background())
	if got.Checkable || got.UpdateAvailable || got.Latest != "" {
		t.Fatalf("dev build status = %+v, want unchecked with no update", got)
	}
	if got.Version != "unknown" {
		t.Fatalf("Version = %q, want %q", got.Version, "unknown")
	}
}
