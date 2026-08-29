//go:build darwin

package darwin

import (
	"context"
	"sync"
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// This file implements axConn on macOS via the Accessibility API
// (AXUIElement*) and CoreFoundation, loaded through purego (dlopen), with
// no cgo and no Objective-C file. It is the only file in this package
// built for GOOS=darwin; every other file (backend.go, traverse.go,
// actions.go, element.go, conn.go, errors.go, ref.go) is platform-
// independent and exercised by unit tests against a fake axConn on any
// GOOS.
//
// CoreFoundation memory-management discipline: every Copy/Create call
// returns an object this file owns a +1 reference to. An object decoded
// into a plain Go value (string/bool/float64/Point/Size) is CFReleased
// immediately after decoding. An object that becomes part of a live
// axRef handed back to the rest of the package (an AXUIElementRef for a
// child/window/parent/app) is instead registered in realAXConn's
// toRelease table via own()/retainAndOwn() and released exactly once, in
// close(). The fixed attribute-name vocabulary (and any action/attribute
// name interned on demand) is cached via cfStringIntern and never
// released, since the interned CFStrings live for the process's lifetime.

// ---- purego bindings -------------------------------------------------

const (
	kCFStringEncodingUTF8 = 0x08000100

	// AXValueType (ApplicationServices/HIServices/AXValue.h).
	kAXValueCGPointType = 1
	kAXValueCGSizeType  = 2

	// CFNumberType (CoreFoundation/CFNumber.h); kCFNumberDoubleType is
	// used unconditionally to decode any CFNumber as a float64.
	kCFNumberDoubleType = 13
)

var (
	// CoreFoundation.
	cfStringCreateWithCString func(alloc uintptr, cStr *byte, encoding uint32) uintptr
	cfStringGetCString        func(theString uintptr, buffer *byte, bufferSize int64, encoding uint32) bool
	cfStringGetLength         func(theString uintptr) int64
	cfArrayGetCount           func(theArray uintptr) int64
	cfArrayGetValueAtIndex    func(theArray uintptr, idx int64) uintptr
	cfArrayCreate             func(allocator uintptr, values *uintptr, numValues int64, callBacks uintptr) uintptr
	cfGetTypeID               func(cf uintptr) uint64
	cfStringGetTypeIDFn       func() uint64
	cfArrayGetTypeIDFn        func() uint64
	cfBooleanGetTypeIDFn      func() uint64
	cfNumberGetTypeIDFn       func() uint64
	cfNumberGetValue          func(number uintptr, theType int32, valuePtr unsafe.Pointer) bool
	cfNumberCreate            func(allocator uintptr, theType int32, valuePtr unsafe.Pointer) uintptr
	cfBooleanGetValue         func(boolean uintptr) bool
	cfRelease                 func(cf uintptr)
	cfRetain                  func(cf uintptr) uintptr
	cfDictionaryCreate        func(allocator uintptr, keys *uintptr, values *uintptr, numValues int64, keyCallBacks uintptr, valueCallBacks uintptr) uintptr

	// ApplicationServices / HIServices (AX API).
	axUIElementCreateApplication         func(pid int32) uintptr
	axUIElementCreateSystemWide          func() uintptr
	axUIElementCopyAttributeValue        func(element uintptr, attribute uintptr, value *uintptr) int32
	axUIElementCopyMultipleAttributeVals func(element uintptr, attributes uintptr, options uint32, values *uintptr) int32
	axUIElementCopyAttributeNames        func(element uintptr, names *uintptr) int32
	axUIElementCopyActionNames           func(element uintptr, names *uintptr) int32
	axUIElementPerformAction             func(element uintptr, action uintptr) int32
	axUIElementSetAttributeValue         func(element uintptr, attribute uintptr, value uintptr) int32
	axUIElementGetPid                    func(element uintptr, pid *int32) int32
	axUIElementCopyElementAtPosition     func(application uintptr, x float32, y float32, element *uintptr) int32
	axIsProcessTrustedFn                 func() bool
	axIsProcessTrustedWithOptionsFn      func(options uintptr) bool
	axUIElementGetTypeIDFn               func() uint64
	axValueGetValue                      func(value uintptr, theType int32, valuePtr unsafe.Pointer) bool
	axValueGetType                       func(value uintptr) int32

	// Cached CFTypeID discriminators, resolved once in init().
	cfStringTypeID  uint64
	cfArrayTypeID   uint64
	cfBooleanTypeID uint64
	cfNumberTypeID  uint64
	axUIElementType uint64

	// Cached singleton/struct symbol addresses.
	cfTypeArrayCallBacksAddr     uintptr
	cfTypeDictKeyCallBacksAddr   uintptr
	cfTypeDictValueCallBacksAddr uintptr
	cfBooleanTrueRef             uintptr
	cfBooleanFalseRef            uintptr

	darwinLoadErr error
)

func mustDlsym(handle uintptr, name string) uintptr {
	addr, err := purego.Dlsym(handle, name)
	if err != nil {
		if darwinLoadErr == nil {
			darwinLoadErr = err
		}
		return 0
	}
	return addr
}

// readCFPtrSymbol reads the pointer VALUE stored at an extern CFTypeRef
// symbol (e.g. `extern const CFStringRef kAXTrustedCheckOptionPrompt;` or
// `extern const CFBooleanRef kCFBooleanTrue;`): the symbol address holds a
// pointer that must be dereferenced once to get the actual CF object,
// unlike a symbol that IS a struct (e.g. kCFTypeArrayCallBacks), whose
// address is passed to C directly with no dereference.
func readCFPtrSymbol(handle uintptr, name string) uintptr {
	addr := mustDlsym(handle, name)
	if addr == 0 {
		return 0
	}
	// addr is a raw C symbol address returned by dlsym as a uintptr (the
	// purego calling convention for every pointer-typed value in this
	// cgo-free FFI style); converting it back to unsafe.Pointer to
	// dereference the extern global it names is the standard,
	// unavoidable pattern here, the same class `go vet` already flags as
	// a false positive in screen_darwin.go's imageFromDisplay (Phase 3).
	// The symbol is a process-lifetime CoreFoundation global (e.g.
	// kCFBooleanTrue), so the read is safe with no ownership concerns.
	return *(*uintptr)(unsafe.Pointer(addr))
}

func init() {
	cfHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		darwinLoadErr = err
		return
	}
	purego.RegisterLibFunc(&cfStringCreateWithCString, cfHandle, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfStringGetCString, cfHandle, "CFStringGetCString")
	purego.RegisterLibFunc(&cfStringGetLength, cfHandle, "CFStringGetLength")
	purego.RegisterLibFunc(&cfArrayGetCount, cfHandle, "CFArrayGetCount")
	purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cfHandle, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&cfArrayCreate, cfHandle, "CFArrayCreate")
	purego.RegisterLibFunc(&cfGetTypeID, cfHandle, "CFGetTypeID")
	purego.RegisterLibFunc(&cfStringGetTypeIDFn, cfHandle, "CFStringGetTypeID")
	purego.RegisterLibFunc(&cfArrayGetTypeIDFn, cfHandle, "CFArrayGetTypeID")
	purego.RegisterLibFunc(&cfBooleanGetTypeIDFn, cfHandle, "CFBooleanGetTypeID")
	purego.RegisterLibFunc(&cfNumberGetTypeIDFn, cfHandle, "CFNumberGetTypeID")
	purego.RegisterLibFunc(&cfNumberGetValue, cfHandle, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfNumberCreate, cfHandle, "CFNumberCreate")
	purego.RegisterLibFunc(&cfBooleanGetValue, cfHandle, "CFBooleanGetValue")
	purego.RegisterLibFunc(&cfRelease, cfHandle, "CFRelease")
	purego.RegisterLibFunc(&cfRetain, cfHandle, "CFRetain")
	purego.RegisterLibFunc(&cfDictionaryCreate, cfHandle, "CFDictionaryCreate")

	cfTypeArrayCallBacksAddr = mustDlsym(cfHandle, "kCFTypeArrayCallBacks")
	cfTypeDictKeyCallBacksAddr = mustDlsym(cfHandle, "kCFTypeDictionaryKeyCallBacks")
	cfTypeDictValueCallBacksAddr = mustDlsym(cfHandle, "kCFTypeDictionaryValueCallBacks")
	cfBooleanTrueRef = readCFPtrSymbol(cfHandle, "kCFBooleanTrue")
	cfBooleanFalseRef = readCFPtrSymbol(cfHandle, "kCFBooleanFalse")

	asHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		darwinLoadErr = err
		return
	}
	purego.RegisterLibFunc(&axUIElementCreateApplication, asHandle, "AXUIElementCreateApplication")
	purego.RegisterLibFunc(&axUIElementCreateSystemWide, asHandle, "AXUIElementCreateSystemWide")
	purego.RegisterLibFunc(&axUIElementCopyAttributeValue, asHandle, "AXUIElementCopyAttributeValue")
	purego.RegisterLibFunc(&axUIElementCopyMultipleAttributeVals, asHandle, "AXUIElementCopyMultipleAttributeValues")
	purego.RegisterLibFunc(&axUIElementCopyAttributeNames, asHandle, "AXUIElementCopyAttributeNames")
	purego.RegisterLibFunc(&axUIElementCopyActionNames, asHandle, "AXUIElementCopyActionNames")
	purego.RegisterLibFunc(&axUIElementPerformAction, asHandle, "AXUIElementPerformAction")
	purego.RegisterLibFunc(&axUIElementSetAttributeValue, asHandle, "AXUIElementSetAttributeValue")
	purego.RegisterLibFunc(&axUIElementGetPid, asHandle, "AXUIElementGetPid")
	purego.RegisterLibFunc(&axUIElementCopyElementAtPosition, asHandle, "AXUIElementCopyElementAtPosition")
	purego.RegisterLibFunc(&axIsProcessTrustedFn, asHandle, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&axIsProcessTrustedWithOptionsFn, asHandle, "AXIsProcessTrustedWithOptions")
	purego.RegisterLibFunc(&axUIElementGetTypeIDFn, asHandle, "AXUIElementGetTypeID")
	purego.RegisterLibFunc(&axValueGetValue, asHandle, "AXValueGetValue")
	purego.RegisterLibFunc(&axValueGetType, asHandle, "AXValueGetType")

	if darwinLoadErr == nil {
		cfStringTypeID = cfStringGetTypeIDFn()
		cfArrayTypeID = cfArrayGetTypeIDFn()
		cfBooleanTypeID = cfBooleanGetTypeIDFn()
		cfNumberTypeID = cfNumberGetTypeIDFn()
		axUIElementType = axUIElementGetTypeIDFn()
	}
}

func checkLoaded() error {
	if darwinLoadErr != nil {
		return core.NewPlatformNotSupportedError("could not load ApplicationServices/CoreFoundation: " + darwinLoadErr.Error())
	}
	return nil
}

// ---- CFString helpers --------------------------------------------------

func cStringPtr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

// cfStringCreateTransient creates a new, non-interned CFStringRef the
// caller must CFRelease after use (e.g. a one-off text value for
// AXUIElementSetAttributeValue).
func cfStringCreateTransient(s string) uintptr {
	return cfStringCreateWithCString(0, cStringPtr(s), kCFStringEncodingUTF8)
}

// cfStringToGo decodes a CFStringRef into a Go string without taking or
// releasing a reference (the caller owns that lifecycle).
func cfStringToGo(ref uintptr) string {
	if ref == 0 {
		return ""
	}
	length := cfStringGetLength(ref)
	bufSize := length*4 + 1
	if bufSize < 16 {
		bufSize = 16
	}
	buf := make([]byte, bufSize)
	if !cfStringGetCString(ref, &buf[0], bufSize, kCFStringEncodingUTF8) {
		return ""
	}
	if idx := indexByte(buf, 0); idx >= 0 {
		return string(buf[:idx])
	}
	return string(buf)
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// ---- realAXConn ---------------------------------------------------------

// realAXConn is the purego-backed axConn implementation. It interns the
// fixed, small vocabulary of attribute/action-name CFStrings once (never
// released) and owns a handle table of every AXUIElementRef it has handed
// out as part of a live axRef, released exactly once in close().
type realAXConn struct {
	internMu sync.Mutex
	interned map[string]uintptr

	handleMu  sync.Mutex
	toRelease []uintptr
}

func newRealAXConn() *realAXConn {
	return &realAXConn{interned: make(map[string]uintptr)}
}

// cfStringIntern returns the cached CFStringRef for s, creating (and
// caching forever) one on first use. Interned CFStrings are never
// released: the vocabulary of attribute and action names is small and
// bounded, and the connection lives for the process's lifetime.
func (c *realAXConn) cfStringIntern(s string) uintptr {
	c.internMu.Lock()
	defer c.internMu.Unlock()
	if ref, ok := c.interned[s]; ok {
		return ref
	}
	ref := cfStringCreateTransient(s)
	c.interned[s] = ref
	return ref
}

// own registers ref (already carrying a +1 reference this connection is
// responsible for) for release in close().
func (c *realAXConn) own(ref uintptr) {
	if ref == 0 {
		return
	}
	c.handleMu.Lock()
	c.toRelease = append(c.toRelease, ref)
	c.handleMu.Unlock()
}

// retainAndOwn CFRetains a borrowed ref (e.g. one read via
// CFArrayGetValueAtIndex, which does not transfer ownership) and then
// owns it, so it survives past the call that produced it and is released
// exactly once in close().
func (c *realAXConn) retainAndOwn(ref uintptr) {
	if ref == 0 {
		return
	}
	cfRetain(ref)
	c.own(ref)
}

func boolCFRef(v bool) uintptr {
	if v {
		return cfBooleanTrueRef
	}
	return cfBooleanFalseRef
}

// decodeCFValue converts one CFTypeRef returned by
// AXUIElementCopyMultipleAttributeValues into a plain Go value understood
// by element.go's parseFetchedNode, following the ownership discipline
// documented at the top of this file. pid is the owning application's pid,
// stamped onto any AXUIElementRef (AXParent/AXChildren/AXWindows) decoded
// here.
func (c *realAXConn) decodeCFValue(item uintptr, pid int32) (any, bool) {
	if item == 0 {
		return nil, false
	}
	id := cfGetTypeID(item)
	switch id {
	case cfStringTypeID:
		s := cfStringToGo(item)
		cfRelease(item)
		return s, true
	case cfBooleanTypeID:
		v := cfBooleanGetValue(item)
		cfRelease(item)
		return v, true
	case cfNumberTypeID:
		var d float64
		cfNumberGetValue(item, kCFNumberDoubleType, unsafe.Pointer(&d))
		cfRelease(item)
		return d, true
	case axUIElementType:
		c.own(item)
		return axRef{PID: pid, Handle: item}, true
	case cfArrayTypeID:
		n := cfArrayGetCount(item)
		refs := make([]axRef, 0, n)
		for i := int64(0); i < n; i++ {
			child := cfArrayGetValueAtIndex(item, i)
			if child == 0 || cfGetTypeID(child) != axUIElementType {
				continue
			}
			c.retainAndOwn(child)
			refs = append(refs, axRef{PID: pid, Handle: child})
		}
		cfRelease(item)
		return refs, true
	default:
		// AXValueRef (CGPoint/CGSize/...) shares no fixed CFTypeID
		// constant exposed by CoreFoundation itself (AXValueGetTypeID is
		// not queried here to keep the discriminator set small); instead
		// any object that is not one of the CF primitive types above is
		// assumed to be an AXValueRef and probed via AXValueGetType,
		// which safely returns kAXValueIllegalType-ish/false for anything
		// that is not actually an AXValueRef.
		t := axValueGetType(item)
		switch t {
		case kAXValueCGPointType:
			var p Point
			if axValueGetValue(item, t, unsafe.Pointer(&p)) {
				cfRelease(item)
				return p, true
			}
		case kAXValueCGSizeType:
			var s Size
			if axValueGetValue(item, t, unsafe.Pointer(&s)) {
				cfRelease(item)
				return s, true
			}
		}
		cfRelease(item)
		return nil, false
	}
}

// attributes implements axConn.attributes via one
// AXUIElementCopyMultipleAttributeValues round trip.
func (c *realAXConn) attributes(ctx context.Context, ref axRef, names []string) (map[string]any, error) {
	if err := checkLoaded(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return map[string]any{}, nil
	}
	cfNames := make([]uintptr, len(names))
	for i, n := range names {
		cfNames[i] = c.cfStringIntern(n)
	}
	arr := cfArrayCreate(0, &cfNames[0], int64(len(cfNames)), cfTypeArrayCallBacksAddr)
	if arr == 0 {
		return nil, core.NewActionFailedError("could not build CFArray of attribute names")
	}
	defer cfRelease(arr)

	var valuesArr uintptr
	code := axUIElementCopyMultipleAttributeVals(ref.Handle, arr, 0, &valuesArr)
	if code != int32(AXErrorSuccess) {
		if de := mapAXError(AXError(code), "AXUIElementCopyMultipleAttributeValues"); de != nil {
			return nil, de
		}
	}
	if valuesArr == 0 {
		return map[string]any{}, nil
	}
	defer cfRelease(valuesArr)

	count := cfArrayGetCount(valuesArr)
	out := make(map[string]any, count)
	for i := int64(0); i < count && int(i) < len(names); i++ {
		item := cfArrayGetValueAtIndex(valuesArr, i)
		if val, ok := c.decodeCFValue(item, ref.PID); ok {
			out[names[i]] = val
		}
	}
	return out, nil
}

// actionNames implements axConn.actionNames.
func (c *realAXConn) actionNames(ctx context.Context, ref axRef) ([]string, error) {
	if err := checkLoaded(); err != nil {
		return nil, err
	}
	var namesArr uintptr
	code := axUIElementCopyActionNames(ref.Handle, &namesArr)
	if code != int32(AXErrorSuccess) {
		if de := mapAXError(AXError(code), "AXUIElementCopyActionNames"); de != nil {
			return nil, de
		}
	}
	if namesArr == 0 {
		return nil, nil
	}
	defer cfRelease(namesArr)
	n := cfArrayGetCount(namesArr)
	out := make([]string, 0, n)
	for i := int64(0); i < n; i++ {
		item := cfArrayGetValueAtIndex(namesArr, i)
		out = append(out, cfStringToGo(item))
	}
	return out, nil
}

// performAction implements axConn.performAction.
func (c *realAXConn) performAction(ctx context.Context, ref axRef, name string) error {
	if err := checkLoaded(); err != nil {
		return err
	}
	cfName := c.cfStringIntern(name)
	code := axUIElementPerformAction(ref.Handle, cfName)
	if code != int32(AXErrorSuccess) {
		return mapAXError(AXError(code), "AXUIElementPerformAction("+name+")")
	}
	return nil
}

// setAttribute implements axConn.setAttribute.
func (c *realAXConn) setAttribute(ctx context.Context, ref axRef, attr string, value any) error {
	if err := checkLoaded(); err != nil {
		return err
	}
	cfAttr := c.cfStringIntern(attr)

	var valRef uintptr
	transient := false
	switch v := value.(type) {
	case string:
		valRef = cfStringCreateTransient(v)
		transient = true
	case bool:
		valRef = boolCFRef(v)
	case float64:
		d := v
		valRef = cfNumberCreate(0, kCFNumberDoubleType, unsafe.Pointer(&d))
		transient = true
	default:
		return core.NewInvalidArgsError("unsupported attribute value type for AXUIElementSetAttributeValue")
	}
	if transient {
		defer cfRelease(valRef)
	}

	code := axUIElementSetAttributeValue(ref.Handle, cfAttr, valRef)
	if code != int32(AXErrorSuccess) {
		return mapAXError(AXError(code), "AXUIElementSetAttributeValue("+attr+")")
	}
	return nil
}

// appElement implements axConn.appElement via AXUIElementCreateApplication.
func (c *realAXConn) appElement(ctx context.Context, pid int32) (axRef, error) {
	if err := checkLoaded(); err != nil {
		return axRef{}, err
	}
	elem := axUIElementCreateApplication(pid)
	if elem == 0 {
		return axRef{}, core.NewAppNotFoundError("AXUIElementCreateApplication returned no element for pid " + itoa(pid))
	}
	c.own(elem)
	return axRef{PID: pid, Handle: elem}, nil
}

func itoa(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// runningApps implements axConn.runningApps by listing every process via
// the kern.proc.all sysctl (golang.org/x/sys/unix.SysctlKinfoProcSlice),
// no cgo required. This is the "PID enumeration" option the plan offers
// as an alternative to binding CGWindowListCopyWindowInfo: it needs no
// extra framework load, decodes into a typed Go struct (KinfoProc) x/sys
// already ships for darwin, and gives every candidate pid to try
// AXUIElementCreateApplication against — Apps() itself, not this call,
// is what determines whether a pid actually exposes a usable
// accessibility tree (empty AXWindows is a normal, non-error outcome for
// a backgrounded/non-GUI process).
func (c *realAXConn) runningApps(ctx context.Context) ([]appProc, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, core.NewActionFailedError("kern.proc.all sysctl failed: " + err.Error())
	}
	out := make([]appProc, 0, len(procs))
	for _, p := range procs {
		pid := p.Proc.P_pid
		if pid <= 0 {
			continue
		}
		name := commToString(p.Proc.P_comm[:])
		out = append(out, appProc{PID: pid, Name: name})
	}
	return out, nil
}

func commToString(b []byte) string {
	if idx := indexByte(b, 0); idx >= 0 {
		b = b[:idx]
	}
	return string(b)
}

// trusted implements axConn.trusted.
func (c *realAXConn) trusted(ctx context.Context) bool {
	if err := checkLoaded(); err != nil {
		return false
	}
	return axIsProcessTrustedFn()
}

// close implements axConn.close: releases every AXUIElementRef this
// connection ever took ownership of (own()/retainAndOwn()). Interned
// CFStrings are deliberately NOT released here (see cfStringIntern's
// doc comment); they are a small, bounded, process-lifetime cache.
func (c *realAXConn) close() error {
	c.handleMu.Lock()
	toRelease := c.toRelease
	c.toRelease = nil
	c.handleMu.Unlock()
	for _, ref := range toRelease {
		cfRelease(ref)
	}
	return nil
}

// ---- opt-in trust-prompt entry point ------------------------------------

// PromptForAccessibilityTrust calls AXIsProcessTrustedWithOptions with
// kAXTrustedCheckOptionPrompt set to true, which makes macOS show the
// user the "grant Accessibility access" system dialog. Per the plan, the
// backend never calls this on its own (Available/every other call site
// uses the silent AXIsProcessTrusted); this function exists purely as the
// documented, explicit opt-in a future caller (e.g. a "request permission"
// UI action) can invoke deliberately.
func PromptForAccessibilityTrust() bool {
	if err := checkLoaded(); err != nil || cfDictionaryCreate == nil {
		return false
	}
	asHandle, err := purego.Dlopen(
		"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		return false
	}
	promptKey := readCFPtrSymbol(asHandle, "kAXTrustedCheckOptionPrompt")
	if promptKey == 0 {
		return axIsProcessTrustedFn()
	}
	keys := []uintptr{promptKey}
	values := []uintptr{cfBooleanTrueRef}
	dict := cfDictionaryCreate(0, &keys[0], &values[0], 1, cfTypeDictKeyCallBacksAddr, cfTypeDictValueCallBacksAddr)
	if dict == 0 {
		return axIsProcessTrustedFn()
	}
	defer cfRelease(dict)
	return axIsProcessTrustedWithOptionsFn(dict)
}

// NewBackend constructs the macOS AXUIElement core.Backend. It never
// fails to construct even when the frameworks could not be dlopen'd or
// the process is not accessibility-trusted; those conditions surface as
// PLATFORM_NOT_SUPPORTED / PERM_DENIED from individual calls (Available in
// particular), matching every other backend's construction contract.
func NewBackend() (core.Backend, error) {
	return newBackendWithConn(newRealAXConn()), nil
}
