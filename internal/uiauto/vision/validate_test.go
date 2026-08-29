package vision

import (
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestValidateCoordinatesEmptyBoundsSkipsValidation(t *testing.T) {
	if err := ValidateCoordinates(99999, 99999, nil); err != nil {
		t.Fatalf("expected nil error when bounds are unknown, got %v", err)
	}
}

func TestValidateCoordinatesWithinBounds(t *testing.T) {
	bounds := []core.Bounds{{X: 0, Y: 0, W: 1920, H: 1080}}
	if err := ValidateCoordinates(100, 100, bounds); err != nil {
		t.Fatalf("expected in-bounds coordinates to validate, got %v", err)
	}
	// Right/bottom edges are exclusive.
	if err := ValidateCoordinates(1919, 1079, bounds); err != nil {
		t.Fatalf("expected edge-inclusive coordinate to validate, got %v", err)
	}
}

func TestValidateCoordinatesOutOfBounds(t *testing.T) {
	bounds := []core.Bounds{{X: 0, Y: 0, W: 1920, H: 1080}}
	err := ValidateCoordinates(1920, 500, bounds)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("err = %v, want INVALID_ARGS", err)
	}
}

func TestValidateCoordinatesMultiMonitorSecondDisplay(t *testing.T) {
	bounds := []core.Bounds{
		{X: 0, Y: 0, W: 1920, H: 1080},
		{X: 1920, Y: 0, W: 1920, H: 1080},
	}
	if err := ValidateCoordinates(2500, 200, bounds); err != nil {
		t.Fatalf("expected coordinate on second display to validate, got %v", err)
	}
}

func TestValidateCoordinatesNegative(t *testing.T) {
	bounds := []core.Bounds{{X: 0, Y: 0, W: 1920, H: 1080}}
	err := ValidateCoordinates(-1, 0, bounds)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("err = %v, want INVALID_ARGS", err)
	}
}
