//go:build !windows && !linux && !darwin

package input

import "github.com/digiogithub/pando/internal/uiauto/core"

// unsupportedInput is the PhysicalInput implementation for platforms this
// package has no native backend for. Every method reports
// PLATFORM_NOT_SUPPORTED.
type unsupportedInput struct{}

func (unsupportedInput) Click(x, y int) error {
	return core.NewPlatformNotSupportedError("physical mouse input is not implemented on this platform")
}

func (unsupportedInput) MoveMouse(x, y int) error {
	return core.NewPlatformNotSupportedError("physical mouse input is not implemented on this platform")
}

func (unsupportedInput) TypeText(s string) error {
	return core.NewPlatformNotSupportedError("physical keyboard input is not implemented on this platform")
}

func (unsupportedInput) PressKey(key string) error {
	return core.NewPlatformNotSupportedError("physical keyboard input is not implemented on this platform")
}

func (unsupportedInput) Scroll(x, y, amount int) error {
	return core.NewPlatformNotSupportedError("physical mouse input is not implemented on this platform")
}

// New constructs the PhysicalInput implementation for this platform. On an
// unsupported platform it never fails to construct; every call simply
// reports PLATFORM_NOT_SUPPORTED.
func New() (core.PhysicalInput, error) {
	return unsupportedInput{}, nil
}

// Capabilities probes what this platform's physical input implementation
// can actually do. Always all-false here.
func Capabilities() core.Capabilities {
	return core.Capabilities{}
}
