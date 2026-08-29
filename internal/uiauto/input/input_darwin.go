//go:build darwin

package input

import (
	"unicode/utf16"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/ebitengine/purego"
)

// This file implements core.PhysicalInput on macOS via CoreGraphics
// CGEvent* functions, loaded through purego (dlopen), with no cgo. Every
// CGEventRef/CGEventSourceRef this file creates is released with CFRelease
// once it has been posted, to avoid leaking CoreFoundation objects.

type cgPoint struct{ X, Y float64 }

const (
	kCGHIDEventTap = 0

	kCGEventLeftMouseDown  = 1
	kCGEventLeftMouseUp    = 2
	kCGEventMouseMoved     = 5
	kCGEventScrollWheel    = 22
	kCGMouseButtonLeft     = 0
	kCGScrollEventUnitLine = 1

	kCGEventSourceStateHIDSystemState = 1

	flagMaskAlternate = 1 << 19 // kCGEventFlagMaskAlternate
	flagMaskShift     = 1 << 17 // kCGEventFlagMaskShift
	flagMaskControl   = 1 << 18 // kCGEventFlagMaskControl
	flagMaskCommand   = 1 << 20 // kCGEventFlagMaskCommand
)

var (
	cgEventCreateMouseEvent         func(source uintptr, mouseType uint32, point cgPoint, mouseButton uint32) uintptr
	cgEventCreateScrollWheelEvent   func(source uintptr, units uint32, wheelCount uint32, wheel1 int32) uintptr
	cgEventPost                     func(tap uint32, event uintptr)
	cgEventCreateKeyboardEvent      func(source uintptr, virtualKey uint16, keyDown bool) uintptr
	cgEventKeyboardSetUnicodeString func(event uintptr, length uintptr, unicodeString *uint16)
	cgEventSetFlags                 func(event uintptr, flags uint64)
	cgEventSetLocation              func(event uintptr, point cgPoint)
	cgWarpMouseCursorPosition       func(point cgPoint) int32
	cgEventSourceCreate             func(stateID int32) uintptr
	cfRelease                       func(cf uintptr)
	axIsProcessTrusted              func() bool

	darwinLoadErr error
)

func init() {
	cgHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		darwinLoadErr = err
		return
	}
	purego.RegisterLibFunc(&cgEventCreateMouseEvent, cgHandle, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEvent, cgHandle, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventPost, cgHandle, "CGEventPost")
	purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cgHandle, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeString, cgHandle, "CGEventKeyboardSetUnicodeString")
	purego.RegisterLibFunc(&cgEventSetFlags, cgHandle, "CGEventSetFlags")
	purego.RegisterLibFunc(&cgEventSetLocation, cgHandle, "CGEventSetLocation")
	purego.RegisterLibFunc(&cgWarpMouseCursorPosition, cgHandle, "CGWarpMouseCursorPosition")
	purego.RegisterLibFunc(&cgEventSourceCreate, cgHandle, "CGEventSourceCreate")

	cfHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		darwinLoadErr = err
		return
	}
	purego.RegisterLibFunc(&cfRelease, cfHandle, "CFRelease")

	asHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err == nil {
		purego.RegisterLibFunc(&axIsProcessTrusted, asHandle, "AXIsProcessTrusted")
	}
}

// namedVirtualKeys maps the canonical named-key vocabulary (see keys.go) to
// macOS (Carbon/HIToolbox) virtual key codes for a US ANSI keyboard layout.
var namedVirtualKeys = map[string]uint16{
	"enter": 0x24, "tab": 0x30, "escape": 0x35, "space": 0x31,
	"backspace": 0x33, "delete": 0x75,
	"up": 0x7E, "down": 0x7D, "left": 0x7B, "right": 0x7C,
	"home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79,
	"capslock": 0x39,
	"f1":       0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76,
	"f5": 0x60, "f6": 0x61, "f7": 0x62, "f8": 0x64,
	"f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
}

// letterAndDigitVirtualKeys maps a-z/0-9 to their US ANSI virtual key
// codes (these are not sequential like Windows VK codes or ASCII).
var letterAndDigitVirtualKeys = map[rune]uint16{
	'a': 0x00, 'b': 0x0B, 'c': 0x08, 'd': 0x02, 'e': 0x0E, 'f': 0x03,
	'g': 0x05, 'h': 0x04, 'i': 0x22, 'j': 0x26, 'k': 0x28, 'l': 0x25,
	'm': 0x2E, 'n': 0x2D, 'o': 0x1F, 'p': 0x23, 'q': 0x0C, 'r': 0x0F,
	's': 0x01, 't': 0x11, 'u': 0x20, 'v': 0x09, 'w': 0x0D, 'x': 0x07,
	'y': 0x10, 'z': 0x06,
	'0': 0x1D, '1': 0x12, '2': 0x13, '3': 0x14, '4': 0x15,
	'5': 0x17, '6': 0x16, '7': 0x1A, '8': 0x1C, '9': 0x19,
}

// darwinInput implements core.PhysicalInput for macOS via CoreGraphics.
type darwinInput struct{}

func (darwinInput) checkLoaded() error {
	if darwinLoadErr != nil {
		return core.NewPlatformNotSupportedError("could not load CoreGraphics/ApplicationServices: " + darwinLoadErr.Error())
	}
	return nil
}

// checkTrust reports PERM_DENIED when the process is not trusted for
// accessibility/input synthesis (System Settings > Privacy & Security >
// Accessibility). When the probe itself is unavailable, trust is assumed
// (the subsequent CGEventPost will simply be a no-op if untrusted).
func (darwinInput) checkTrust() error {
	if axIsProcessTrusted == nil {
		return nil
	}
	if !axIsProcessTrusted() {
		return core.NewPermDeniedError("Pando is not trusted for Accessibility input synthesis")
	}
	return nil
}

func (d darwinInput) withSource(fn func(source uintptr)) error {
	if err := d.checkLoaded(); err != nil {
		return err
	}
	if err := d.checkTrust(); err != nil {
		return err
	}
	source := cgEventSourceCreate(kCGEventSourceStateHIDSystemState)
	defer func() {
		if source != 0 {
			cfRelease(source)
		}
	}()
	fn(source)
	return nil
}

func postAndRelease(event uintptr) {
	if event == 0 {
		return
	}
	cgEventPost(kCGHIDEventTap, event)
	cfRelease(event)
}

func (d darwinInput) MoveMouse(x, y int) error {
	if err := d.checkLoaded(); err != nil {
		return err
	}
	if err := d.checkTrust(); err != nil {
		return err
	}
	cgWarpMouseCursorPosition(cgPoint{X: float64(x), Y: float64(y)})
	return nil
}

func (d darwinInput) Click(x, y int) error {
	return d.withSource(func(source uintptr) {
		point := cgPoint{X: float64(x), Y: float64(y)}
		down := cgEventCreateMouseEvent(source, kCGEventLeftMouseDown, point, kCGMouseButtonLeft)
		postAndRelease(down)
		up := cgEventCreateMouseEvent(source, kCGEventLeftMouseUp, point, kCGMouseButtonLeft)
		postAndRelease(up)
	})
}

func (d darwinInput) Scroll(x, y, amount int) error {
	return d.withSource(func(source uintptr) {
		cgWarpMouseCursorPosition(cgPoint{X: float64(x), Y: float64(y)})
		ev := cgEventCreateScrollWheelEvent(source, kCGScrollEventUnitLine, 1, int32(amount))
		postAndRelease(ev)
	})
}

func (d darwinInput) TypeText(s string) error {
	return d.withSource(func(source uintptr) {
		for _, r := range s {
			units := utf16.Encode([]rune{r})
			down := cgEventCreateKeyboardEvent(source, 0, true)
			cgEventKeyboardSetUnicodeString(down, uintptr(len(units)), &units[0])
			postAndRelease(down)
			up := cgEventCreateKeyboardEvent(source, 0, false)
			cgEventKeyboardSetUnicodeString(up, uintptr(len(units)), &units[0])
			postAndRelease(up)
		}
	})
}

func (d darwinInput) PressKey(key string) error {
	chord, err := ParseChord(key)
	if err != nil {
		return err
	}

	var vk uint16
	if named, ok := namedVirtualKeys[chord.Key]; ok {
		vk = named
	} else {
		runes := []rune(chord.Key)
		if len(runes) != 1 {
			return core.NewInvalidArgsError("unrecognized key name " + chord.Key)
		}
		mapped, ok := letterAndDigitVirtualKeys[runes[0]]
		if !ok {
			// Fall back to typing it as unicode text; there is no stable
			// virtual-key code for arbitrary punctuation across layouts.
			return d.TypeText(chord.Key)
		}
		vk = mapped
	}

	var flags uint64
	if chord.HasModifier(ModCtrl) {
		flags |= flagMaskControl
	}
	if chord.HasModifier(ModAlt) {
		flags |= flagMaskAlternate
	}
	if chord.HasModifier(ModShift) {
		flags |= flagMaskShift
	}
	if chord.HasModifier(ModCmd) {
		flags |= flagMaskCommand
	}

	return d.withSource(func(source uintptr) {
		down := cgEventCreateKeyboardEvent(source, vk, true)
		if flags != 0 {
			cgEventSetFlags(down, flags)
		}
		postAndRelease(down)
		up := cgEventCreateKeyboardEvent(source, vk, false)
		if flags != 0 {
			cgEventSetFlags(up, flags)
		}
		postAndRelease(up)
	})
}

// New constructs the macOS PhysicalInput implementation. It never fails to
// construct even when CoreGraphics could not be dlopen'd or the process is
// not accessibility-trusted; those conditions surface as
// PLATFORM_NOT_SUPPORTED / PERM_DENIED from individual method calls.
func New() (core.PhysicalInput, error) {
	return darwinInput{}, nil
}

// Capabilities probes what this platform's physical input implementation
// can do: CoreGraphics must have loaded and the process must be trusted
// for Accessibility input synthesis.
func Capabilities() core.Capabilities {
	if darwinLoadErr != nil {
		return core.Capabilities{}
	}
	trusted := axIsProcessTrusted == nil || axIsProcessTrusted()
	return core.Capabilities{Mouse: trusted, Keyboard: trusted}
}
