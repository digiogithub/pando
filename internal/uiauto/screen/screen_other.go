//go:build !windows && !linux && !darwin

package screen

import (
	"context"
	"image"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Capture always reports PLATFORM_NOT_SUPPORTED on a platform this
// package has no native capture backend for.
func Capture(ctx context.Context, target Target) (image.Image, error) {
	return nil, core.NewPlatformNotSupportedError("screen capture is not implemented on this platform")
}

// Displays always reports PLATFORM_NOT_SUPPORTED on a platform this
// package has no native capture backend for.
func Displays() ([]DisplayInfo, error) {
	return nil, core.NewPlatformNotSupportedError("display enumeration is not implemented on this platform")
}

// Capabilities is always all-false on a platform this package has no
// native capture backend for.
func Capabilities() core.Capabilities {
	return core.Capabilities{}
}
