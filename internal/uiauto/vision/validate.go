package vision

import (
	"fmt"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// ValidateCoordinates checks that (x,y) falls within at least one of
// bounds (typically the set of currently active display bounds). An empty
// bounds slice means the caller could not determine real display bounds
// (e.g. the screen package could not enumerate displays); validation is
// then skipped -- it is on the caller to decide whether that is
// acceptable rather than silently rejecting every coordinate action.
// Coordinates outside every given Bounds return an INVALID_ARGS
// core.DesktopError.
func ValidateCoordinates(x, y int, bounds []core.Bounds) error {
	if len(bounds) == 0 {
		return nil
	}
	for _, b := range bounds {
		if withinBounds(x, y, b) {
			return nil
		}
	}
	return core.NewInvalidArgsError(fmt.Sprintf(
		"coordinates (%d,%d) are outside the captured display bounds", x, y))
}

func withinBounds(x, y int, b core.Bounds) bool {
	return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H
}
