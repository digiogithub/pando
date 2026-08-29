//go:build darwin

package screen

import (
	"context"
	"fmt"
	"image"
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/ebitengine/purego"
)

// This file implements screen capture on macOS via CoreGraphics
// CGDisplayCreateImage / CGGetActiveDisplayList and the CGImage/CGDataProvider
// accessors, loaded through purego (dlopen), with no cgo. Every CF/CG object
// this file creates (CGImageRef, CGDataProviderRef, CFDataRef) is released
// with CFRelease once its bytes have been copied out.

var (
	cgMainDisplayID        func() uint32
	cgGetActiveDisplayList func(maxDisplays uint32, activeDisplays *uint32, displayCount *uint32) int32
	cgDisplayCreateImage   func(displayID uint32) uintptr
	cgDisplayBounds        func(displayID uint32) cgRect
	cgImageGetWidth        func(image uintptr) uintptr
	cgImageGetHeight       func(image uintptr) uintptr
	cgImageGetBytesPerRow  func(image uintptr) uintptr
	cgImageGetDataProvider func(image uintptr) uintptr
	cgDataProviderCopyData func(provider uintptr) uintptr
	cfDataGetLength        func(data uintptr) int64
	cfDataGetBytePtr       func(data uintptr) uintptr
	cfRelease              func(cf uintptr)
	axIsProcessTrusted     func() bool

	darwinLoadErr error
)

type cgRect struct {
	X, Y, W, H float64
}

func init() {
	cgHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		darwinLoadErr = err
		return
	}
	purego.RegisterLibFunc(&cgMainDisplayID, cgHandle, "CGMainDisplayID")
	purego.RegisterLibFunc(&cgGetActiveDisplayList, cgHandle, "CGGetActiveDisplayList")
	purego.RegisterLibFunc(&cgDisplayCreateImage, cgHandle, "CGDisplayCreateImage")
	purego.RegisterLibFunc(&cgDisplayBounds, cgHandle, "CGDisplayBounds")
	purego.RegisterLibFunc(&cgImageGetWidth, cgHandle, "CGImageGetWidth")
	purego.RegisterLibFunc(&cgImageGetHeight, cgHandle, "CGImageGetHeight")
	purego.RegisterLibFunc(&cgImageGetBytesPerRow, cgHandle, "CGImageGetBytesPerRow")
	purego.RegisterLibFunc(&cgImageGetDataProvider, cgHandle, "CGImageGetDataProvider")
	purego.RegisterLibFunc(&cgDataProviderCopyData, cgHandle, "CGDataProviderCopyData")
	purego.RegisterLibFunc(&cfDataGetLength, cgHandle, "CFDataGetLength")
	purego.RegisterLibFunc(&cfDataGetBytePtr, cgHandle, "CFDataGetBytePtr")

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

func checkLoaded() error {
	if darwinLoadErr != nil {
		return core.NewPlatformNotSupportedError("could not load CoreGraphics: " + darwinLoadErr.Error())
	}
	return nil
}

// imageFromDisplay captures displayID and converts the returned CGImage
// into a Go *image.RGBA, releasing every CoreFoundation object it creates
// along the way.
func imageFromDisplay(displayID uint32) (image.Image, error) {
	cgImg := cgDisplayCreateImage(displayID)
	if cgImg == 0 {
		return nil, core.NewPermDeniedError("CGDisplayCreateImage returned no image; Screen Recording permission may not be granted")
	}
	defer cfRelease(cgImg)

	w := int(cgImageGetWidth(cgImg))
	h := int(cgImageGetHeight(cgImg))
	stride := int(cgImageGetBytesPerRow(cgImg))
	if w <= 0 || h <= 0 {
		return nil, core.NewActionFailedError("captured display image has zero size")
	}

	provider := cgImageGetDataProvider(cgImg)
	if provider == 0 {
		return nil, core.NewActionFailedError("CGImageGetDataProvider returned nil")
	}
	data := cgDataProviderCopyData(provider)
	if data == 0 {
		return nil, core.NewActionFailedError("CGDataProviderCopyData returned nil")
	}
	defer cfRelease(data)

	length := int(cfDataGetLength(data))
	ptr := cfDataGetBytePtr(data)
	if ptr == 0 || length <= 0 {
		return nil, core.NewActionFailedError("captured display image had no backing bytes")
	}
	// ptr is a raw C pointer returned by CFDataGetBytePtr as a uintptr (the
	// purego calling convention for all pointer-typed C return values);
	// converting it back to unsafe.Pointer here is the standard,
	// unavoidable pattern for cgo-free FFI bindings and is safe because the
	// backing CFDataRef is kept alive (via cfRelease being deferred, not
	// called yet) for the lifetime of this slice. `go vet` flags this
	// uintptr->unsafe.Pointer conversion pattern generically; it is a false
	// positive here, and only reachable when cross-vetting for darwin.
	raw := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), length)

	// CGDisplayCreateImage returns 32bpp BGRA (premultiplied) or similar
	// byte order depending on the display's pixel format; the widely
	// compatible assumption on Apple Silicon and Intel Macs alike is
	// little-endian BGRA (matches CGImageAlphaPremultipliedFirst /
	// kCGBitmapByteOrder32Little as used by screen capture APIs).
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srcRow := raw[y*stride : y*stride+w*4]
		dstRow := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			b := srcRow[x*4+0]
			g := srcRow[x*4+1]
			r := srcRow[x*4+2]
			a := srcRow[x*4+3]
			dstRow[x*4+0] = r
			dstRow[x*4+1] = g
			dstRow[x*4+2] = b
			dstRow[x*4+3] = a
		}
	}
	return img, nil
}

func activeDisplayIDs() ([]uint32, error) {
	const maxDisplays = 16
	var ids [maxDisplays]uint32
	var count uint32
	if code := cgGetActiveDisplayList(maxDisplays, &ids[0], &count); code != 0 {
		return nil, core.NewActionFailedError(fmt.Sprintf("CGGetActiveDisplayList failed with error %d", code))
	}
	return ids[:count], nil
}

// Capture captures target: a Region is captured by cropping the primary
// display's full-resolution image to that rectangle; otherwise
// target.Display selects among the active displays (0 = main display).
// WindowID-scoped capture is not implemented (falls back to whole-screen);
// Manager.Screenshot crops to the window's known Bounds itself when a
// window-scoped screenshot is requested.
func Capture(ctx context.Context, target Target) (image.Image, error) {
	if err := checkLoaded(); err != nil {
		return nil, err
	}

	ids, err := activeDisplayIDs()
	if err != nil || len(ids) == 0 {
		return nil, core.NewPlatformNotSupportedError("no active displays are available to capture")
	}
	idx := target.Display
	if idx < 0 || idx >= len(ids) {
		idx = 0
	}
	img, err := imageFromDisplay(ids[idx])
	if err != nil {
		return nil, err
	}
	if target.Region != nil {
		r := target.Region
		rgba, ok := img.(*image.RGBA)
		if !ok {
			return img, nil
		}
		bounds := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(rgba.Bounds())
		return rgba.SubImage(bounds), nil
	}
	return img, nil
}

// Displays enumerates active displays via CGGetActiveDisplayList/CGDisplayBounds.
func Displays() ([]DisplayInfo, error) {
	if err := checkLoaded(); err != nil {
		return nil, err
	}
	ids, err := activeDisplayIDs()
	if err != nil {
		return nil, err
	}
	main := cgMainDisplayID()
	out := make([]DisplayInfo, 0, len(ids))
	for i, id := range ids {
		b := cgDisplayBounds(id)
		out = append(out, DisplayInfo{
			Index: i,
			Name:  fmt.Sprintf("display-%d", id),
			Bounds: core.Bounds{
				X: int(b.X), Y: int(b.Y), W: int(b.W), H: int(b.H),
			},
			Primary: id == main,
		})
	}
	return out, nil
}

// Capabilities reports screenshot availability: requires CoreGraphics to
// have loaded and (best-effort) the process to be trusted for screen
// recording — macOS gates CGDisplayCreateImage on the Screen Recording
// privacy permission independently of Accessibility trust, but there is
// no cheap non-capturing probe for it, so this only reports the load
// failure case as false and lets an actual Capture call surface
// PERM_DENIED if the OS silently returns an empty image.
func Capabilities() core.Capabilities {
	if darwinLoadErr != nil {
		return core.Capabilities{}
	}
	return core.Capabilities{Screenshot: true}
}
