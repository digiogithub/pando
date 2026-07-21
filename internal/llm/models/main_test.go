package models

import (
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

// TestMain keeps the whole package's tests offline: model registration now
// consults the models.dev catalog, and unit tests must never depend on that
// network call (nor on whatever pricing the live catalog happens to publish).
func TestMain(m *testing.M) {
	modelsdev.SetDisabled(true)
	os.Exit(m.Run())
}
