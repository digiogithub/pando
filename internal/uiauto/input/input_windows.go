//go:build windows

package input

import (
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// This file implements core.PhysicalInput on Windows via the raw Win32
// user32.dll SendInput/SetCursorPos APIs, loaded through syscall.NewLazyDLL
// (no cgo). Text is entered as Unicode scan codes (KEYEVENTF_UNICODE) so
// arbitrary text, not just ASCII, can be typed; named keys and chords use
// virtual-key codes so modifier combinations behave like real key presses.

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procVkKeyScanW       = user32.NewProc("VkKeyScanW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	inputMouse    uint32 = 0
	inputKeyboard uint32 = 1

	mouseeventfMove     uint32 = 0x0001
	mouseeventfLeftDown uint32 = 0x0002
	mouseeventfLeftUp   uint32 = 0x0004
	mouseeventfWheel    uint32 = 0x0800
	mouseeventfAbsolute uint32 = 0x8000

	keyeventfKeyUp    uint32 = 0x0002
	keyeventfUnicode  uint32 = 0x0004
	keyeventfExtended uint32 = 0x0001

	wheelDelta = 120

	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkShift   = 0x10
	vkLWin    = 0x5B
)

// namedVirtualKeys maps the canonical named-key vocabulary (see keys.go) to
// Windows virtual-key codes.
var namedVirtualKeys = map[string]uint16{
	"enter": 0x0D, "tab": 0x09, "escape": 0x1B, "space": 0x20,
	"backspace": 0x08, "delete": 0x2E,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22,
	"insert": 0x2D, "capslock": 0x14,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
	"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
	"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
}

// mouseInput mirrors the Win32 MOUSEINPUT struct.
type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// keybdInput mirrors the Win32 KEYBDINPUT struct.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// rawInput mirrors the Win32 INPUT struct: a uint32 type tag followed by a
// tagged union of MOUSEINPUT/KEYBDINPUT/HARDWAREINPUT. Go has no native
// union; the union is represented as a byte array sized to the largest
// variant (MOUSEINPUT, 32 bytes on amd64) and populated in place via
// unsafe.Pointer, which reproduces the exact Win32 ABI layout SendInput
// expects (verified: 4 + 4 padding + 32 = 40 bytes, matching sizeof(INPUT)
// on 64-bit Windows).
type rawInput struct {
	inputType uint32
	_         uint32
	payload   [32]byte
}

func newMouseRawInput(mi mouseInput) rawInput {
	var in rawInput
	in.inputType = inputMouse
	*(*mouseInput)(unsafe.Pointer(&in.payload[0])) = mi
	return in
}

func newKeybdRawInput(ki keybdInput) rawInput {
	var in rawInput
	in.inputType = inputKeyboard
	*(*keybdInput)(unsafe.Pointer(&in.payload[0])) = ki
	return in
}

func sendInputs(inputs []rawInput) error {
	if len(inputs) == 0 {
		return nil
	}
	sz := unsafe.Sizeof(inputs[0])
	ret, _, callErr := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(sz),
	)
	if ret != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput sent %d/%d events: %v", ret, len(inputs), callErr)
	}
	return nil
}

// screenToAbsolute converts a screen-pixel coordinate to the 0..65535
// normalized range MOUSEEVENTF_ABSOLUTE expects.
func screenToAbsolute(x, y int) (int32, int32) {
	sw, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	if sw == 0 {
		sw = 1
	}
	if sh == 0 {
		sh = 1
	}
	ax := int32(float64(x) * 65535 / float64(sw))
	ay := int32(float64(y) * 65535 / float64(sh))
	return ax, ay
}

// winInput implements core.PhysicalInput for Windows.
type winInput struct{}

func (winInput) MoveMouse(x, y int) error {
	ret, _, callErr := procSetCursorPos.Call(uintptr(int32(x)), uintptr(int32(y)))
	if ret == 0 {
		return fmt.Errorf("SetCursorPos failed: %v", callErr)
	}
	return nil
}

func (w winInput) Click(x, y int) error {
	if err := w.MoveMouse(x, y); err != nil {
		return err
	}
	down := newMouseRawInput(mouseInput{dwFlags: mouseeventfLeftDown})
	up := newMouseRawInput(mouseInput{dwFlags: mouseeventfLeftUp})
	return sendInputs([]rawInput{down, up})
}

func (winInput) Scroll(x, y, amount int) error {
	if ret, _, callErr := procSetCursorPos.Call(uintptr(int32(x)), uintptr(int32(y))); ret == 0 {
		return fmt.Errorf("SetCursorPos failed: %v", callErr)
	}
	wheel := newMouseRawInput(mouseInput{
		mouseData: uint32(int32(amount * wheelDelta)),
		dwFlags:   mouseeventfWheel,
	})
	return sendInputs([]rawInput{wheel})
}

func (winInput) TypeText(s string) error {
	units := utf16.Encode([]rune(s))
	inputs := make([]rawInput, 0, len(units)*2)
	for _, u := range units {
		down := newKeybdRawInput(keybdInput{wScan: u, dwFlags: keyeventfUnicode})
		up := newKeybdRawInput(keybdInput{wScan: u, dwFlags: keyeventfUnicode | keyeventfKeyUp})
		inputs = append(inputs, down, up)
	}
	return sendInputs(inputs)
}

// vkForRune resolves the virtual-key code (and whether Shift must be held)
// for a single-rune key via VkKeyScanW.
func vkForRune(r rune) (vk uint16, shift bool, ok bool) {
	ret, _, _ := procVkKeyScanW.Call(uintptr(r))
	res := int16(ret)
	if res == -1 {
		return 0, false, false
	}
	vk = uint16(byte(res))
	shiftState := byte(res >> 8)
	shift = shiftState&0x01 != 0
	return vk, shift, true
}

func (winInput) PressKey(key string) error {
	chord, err := ParseChord(key)
	if err != nil {
		return err
	}

	var vk uint16
	needShift := chord.HasModifier(ModShift)
	if named, ok := namedVirtualKeys[chord.Key]; ok {
		vk = named
	} else {
		runes := []rune(chord.Key)
		if len(runes) != 1 {
			return core.NewInvalidArgsError("unrecognized key name " + chord.Key)
		}
		resolved, shift, ok := vkForRune(runes[0])
		if !ok {
			return core.NewActionFailedError("could not resolve virtual-key code for " + chord.Key)
		}
		vk = resolved
		needShift = needShift || shift
	}

	var downs, ups []rawInput
	pressMod := func(modVk uint16) {
		downs = append(downs, newKeybdRawInput(keybdInput{wVk: modVk}))
		ups = append([]rawInput{newKeybdRawInput(keybdInput{wVk: modVk, dwFlags: keyeventfKeyUp})}, ups...)
	}
	if chord.HasModifier(ModCtrl) {
		pressMod(vkControl)
	}
	if chord.HasModifier(ModAlt) {
		pressMod(vkMenu)
	}
	if needShift {
		pressMod(vkShift)
	}
	if chord.HasModifier(ModCmd) {
		pressMod(vkLWin)
	}

	downs = append(downs, newKeybdRawInput(keybdInput{wVk: vk}))
	ups = append([]rawInput{newKeybdRawInput(keybdInput{wVk: vk, dwFlags: keyeventfKeyUp})}, ups...)

	all := append(downs, ups...)
	return sendInputs(all)
}

// New constructs the Windows PhysicalInput implementation. It never fails
// to construct (Windows always exposes user32.dll); a call-time failure
// (e.g. SendInput blocked by UIPI) surfaces as an ACTION_FAILED error from
// the failing method instead.
func New() (core.PhysicalInput, error) {
	return winInput{}, nil
}

// Capabilities probes what this platform's physical input implementation
// can do. user32.dll SendInput/SetCursorPos are always available on
// Windows desktop sessions.
func Capabilities() core.Capabilities {
	return core.Capabilities{Mouse: true, Keyboard: true}
}
