//go:build windows

package screen

import (
	"context"
	"fmt"
	"image"
	"syscall"
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// This file implements screen capture on Windows via the GDI
// BitBlt/GetDIBits APIs, loaded through syscall.NewLazyDLL against
// user32.dll/gdi32.dll (no cgo). Multi-monitor enumeration uses
// EnumDisplayMonitors.

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetDesktopWindow    = user32.NewProc("GetDesktopWindow")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

const (
	srcCopy             = 0x00CC0020
	dibRGBColors        = 0
	biRGB               = 0
	monitorInfoFPrimary = 0x00000001
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	// Colors is unused for BI_RGB 32bpp captures; present to match the
	// Win32 BITMAPINFO layout.
	Colors [1]uint32
}

type monitorInfoEx struct {
	Size       uint32
	Monitor    rect
	WorkArea   rect
	Flags      uint32
	DeviceName [32]uint16
}

func captureRegion(x, y, w, h int) (image.Image, error) {
	desktop, _, _ := procGetDesktopWindow.Call()
	hdcScreen, _, _ := procGetDC.Call(desktop)
	if hdcScreen == 0 {
		return nil, core.NewActionFailedError("GetDC failed")
	}
	defer procReleaseDC.Call(desktop, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, core.NewActionFailedError("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdcMem)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(w), uintptr(h))
	if hBitmap == 0 {
		return nil, core.NewActionFailedError("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hBitmap)

	oldObj, _, _ := procSelectObject.Call(hdcMem, hBitmap)
	defer procSelectObject.Call(hdcMem, oldObj)

	ret, _, callErr := procBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h), hdcScreen, uintptr(int32(x)), uintptr(int32(y)), uintptr(srcCopy))
	if ret == 0 {
		return nil, core.NewActionFailedError(fmt.Sprintf("BitBlt failed: %v", callErr))
	}

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // negative = top-down DIB
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	buf := make([]byte, w*h*4)
	ret, _, callErr = procGetDIBits.Call(
		hdcMem, hBitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bi)),
		uintptr(dibRGBColors),
	)
	if ret == 0 {
		return nil, core.NewActionFailedError(fmt.Sprintf("GetDIBits failed: %v", callErr))
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		b := buf[i*4+0]
		g := buf[i*4+1]
		r := buf[i*4+2]
		img.Pix[i*4+0] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = 0xFF
	}
	return img, nil
}

// Capture captures target: a Region when set, otherwise the bounds of
// target.Display (falling back to the union of all displays' bounding box
// when Display resolution fails). WindowID-scoped capture is not
// implemented (falls back to whole-screen); Manager.Screenshot crops to
// the window's known Bounds itself when a window-scoped screenshot is
// requested.
func Capture(ctx context.Context, target Target) (image.Image, error) {
	if target.Region != nil {
		r := target.Region
		return captureRegion(r.X, r.Y, r.W, r.H)
	}
	displays, err := Displays()
	if err != nil || len(displays) == 0 {
		return nil, core.NewPlatformNotSupportedError("no displays are available to capture")
	}
	idx := target.Display
	if idx < 0 || idx >= len(displays) {
		idx = 0
	}
	b := displays[idx].Bounds
	return captureRegion(b.X, b.Y, b.W, b.H)
}

// Displays enumerates monitors via EnumDisplayMonitors/GetMonitorInfoW.
func Displays() ([]DisplayInfo, error) {
	var out []DisplayInfo
	cb := syscall.NewCallback(func(hMonitor, hdcMonitor uintptr, lprcMonitor uintptr, lParam uintptr) uintptr {
		var mi monitorInfoEx
		mi.Size = uint32(unsafe.Sizeof(mi))
		ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
		if ret != 0 {
			out = append(out, DisplayInfo{
				Index: len(out),
				Name:  syscall.UTF16ToString(mi.DeviceName[:]),
				Bounds: core.Bounds{
					X: int(mi.Monitor.Left), Y: int(mi.Monitor.Top),
					W: int(mi.Monitor.Right - mi.Monitor.Left),
					H: int(mi.Monitor.Bottom - mi.Monitor.Top),
				},
				Primary: mi.Flags&monitorInfoFPrimary != 0,
			})
		}
		return 1 // continue enumeration
	})
	ret, _, callErr := procEnumDisplayMonitors.Call(0, 0, cb, 0)
	if ret == 0 {
		return nil, core.NewActionFailedError(fmt.Sprintf("EnumDisplayMonitors failed: %v", callErr))
	}
	return out, nil
}

// Capabilities reports screenshot availability: GDI BitBlt is always
// available on a Windows desktop session.
func Capabilities() core.Capabilities {
	return core.Capabilities{Screenshot: true}
}
