//go:build windows

package windows

import (
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// comObject is the minimal shape every COM interface pointer this package
// deals with shares: a pointer to a vtable (an array of function pointers),
// exactly like ole.IUnknown itself. UIA-specific interfaces
// (IUIAutomation, IUIAutomationElement, IUIAutomationInvokePattern, ...) are
// not bound by go-ole, so this package treats every such pointer as a raw
// *comObject and calls its vtable slots by hand, reusing go-ole only for
// CoInitializeEx/CoCreateInstance/GUID parsing and the base IUnknown
// AddRef/Release/QueryInterface (which every COM interface shares its first
// three vtable slots with).
type comObject struct {
	vtbl *uintptr
}

func asComObject(u *ole.IUnknown) *comObject {
	return (*comObject)(unsafe.Pointer(u))
}

// vtblFunc reads the function pointer at the given zero-based vtable slot
// index. Every call site in this package documents, in a comment, which
// named vtable slot (per the public UI Automation Client IDL,
// UIAutomationClient.idl / UIAutomationCore.idl) that index corresponds to
// — this backend is compile-verified only and has never run against a real
// COM implementation, so a slot-index drift here is the single highest-risk
// spot in the whole package if Microsoft's public vtable order is
// misremembered.
func (o *comObject) vtblFunc(index int) uintptr {
	base := uintptr(unsafe.Pointer(o.vtbl))
	slot := base + uintptr(index)*unsafe.Sizeof(uintptr(0))
	return *(*uintptr)(unsafe.Pointer(slot))
}

// call invokes the vtable method at index with args appended after the
// implicit `this` pointer, via syscall.SyscallN. It returns the raw HRESULT
// (as a uintptr, callers cast to uint32) and any low-level syscall error
// (essentially never populated for a well-formed vtable call; present for
// completeness/symmetry with syscall.SyscallN's signature).
func (o *comObject) call(index int, args ...uintptr) (hr uintptr, callErr error) {
	fn := o.vtblFunc(index)
	full := make([]uintptr, 0, len(args)+1)
	full = append(full, uintptr(unsafe.Pointer(o)))
	full = append(full, args...)
	r1, _, e := syscall.SyscallN(fn, full...)
	if e != 0 {
		return r1, e
	}
	return r1, nil
}

// Release calls IUnknown::Release (vtable slot 2, shared by every COM
// interface).
func (o *comObject) Release() {
	if o == nil || o.vtbl == nil {
		return
	}
	_, _ = o.call(2)
}

// AddRef calls IUnknown::AddRef (vtable slot 1).
func (o *comObject) AddRef() {
	if o == nil || o.vtbl == nil {
		return
	}
	_, _ = o.call(1)
}

// hresultOf narrows a call()'s raw return value to a uint32 HRESULT.
func hresultOf(v uintptr) uint32 { return uint32(v) }

// bstrToString converts a BSTR (returned by many UIA property getters) to a
// Go string and frees it. A nil pointer yields "".
func bstrToString(b *uint16) string {
	if b == nil {
		return ""
	}
	s := ole.BstrToString(b)
	_ = ole.SysFreeString((*int16)(unsafe.Pointer(b)))
	return s
}

// stringToBSTR allocates a BSTR for passing a string argument (e.g.
// ValuePattern::SetValue, PropertyCondition values) to a raw vtable call.
// The caller is responsible for freeing it once the call returns (UIA
// generally takes its own copy of BSTR arguments, so the allocation is
// freed immediately after the call rather than owned by the callee).
func stringToBSTR(s string) *uint16 {
	return (*uint16)(unsafe.Pointer(ole.SysAllocStringLen(s)))
}

// freeBSTR releases a BSTR allocated by stringToBSTR.
func freeBSTR(b *uint16) {
	if b == nil {
		return
	}
	_ = ole.SysFreeString((*int16)(unsafe.Pointer(b)))
}
