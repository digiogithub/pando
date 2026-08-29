//go:build windows

package windows

import (
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
	ole "github.com/go-ole/go-ole"
)

// IUIAutomationElement vtable slot indices (UIAutomationClient.idl). Slots
// 0-2 are IUnknown.
const (
	idxElementSetFocus               = 3
	idxElementGetRuntimeId           = 4
	idxElementFindAllBuildCache      = 8
	idxElementGetCurrentPattern      = 13
	idxElementGetCachedPropertyValue = 14
	idxElementGetCachedPattern       = 17
)

// IUIAutomationElementArray vtable slot indices.
const (
	idxElementArrayGetLength  = 3
	idxElementArrayGetElement = 4
)

// uiaElement wraps a live IUIAutomationElement COM pointer. It is never
// stored on a core.Element (see doc.go); it only ever lives in the
// backend's handle table (backend_windows.go) or as a short-lived local
// while building that table.
type uiaElement struct {
	obj *comObject
}

func (e *uiaElement) release() {
	if e == nil || e.obj == nil {
		return
	}
	e.obj.Release()
}

// getCachedProperty reads propertyId from the element's local cache
// (populated by the CacheRequest a preceding FindAllBuildCache/
// GetRootElementBuildCache call used) — this is a same-process call, not a
// fresh cross-process round trip, which is the entire point of the
// caching strategy documented in doc.go.
func (e *uiaElement) getCachedProperty(propertyID int32) (ole.VARIANT, error) {
	var v ole.VARIANT
	hr, _ := e.obj.call(idxElementGetCachedPropertyValue, uintptr(propertyID), uintptr(unsafe.Pointer(&v)))
	if hresultOf(hr) != hrOK {
		return v, mapHRESULT("IUIAutomationElement.GetCachedPropertyValue", hresultOf(hr), core.ErrActionFailed)
	}
	return v, nil
}

// variantString reads v as a string, whatever its VT (BSTR is the only
// shape UIA ever returns for the string properties this backend reads).
func variantString(v ole.VARIANT) string {
	if v.VT != ole.VT_BSTR {
		return ""
	}
	return v.ToString()
}

// variantBool reads v as a bool (VT_BOOL).
func variantBool(v ole.VARIANT) bool {
	if v.VT != ole.VT_BOOL {
		return false
	}
	b, _ := v.Value().(bool)
	return b
}

// variantInt32 reads v as an int32 (VT_I4).
func variantInt32(v ole.VARIANT) int32 {
	if v.VT != ole.VT_I4 {
		return 0
	}
	n, _ := v.Value().(int32)
	return n
}

// variantInt32Array reads v as a VT_ARRAY|VT_I4 SAFEARRAY (used for
// RuntimeId).
func variantInt32Array(v ole.VARIANT) []int32 {
	if v.VT&ole.VT_ARRAY == 0 {
		return nil
	}
	arr := v.ToArray()
	if arr == nil {
		return nil
	}
	defer arr.Release()
	vals := arr.ToValueArray()
	out := make([]int32, 0, len(vals))
	for _, x := range vals {
		if n, ok := x.(int32); ok {
			out = append(out, n)
		}
	}
	return out
}

// variantBounds reads v as a VT_ARRAY|VT_R8 SAFEARRAY of 4 doubles
// (left, top, width, height), UIA's wire shape for
// UIA_BoundingRectanglePropertyId.
func variantBounds(v ole.VARIANT) core.Bounds {
	if v.VT&ole.VT_ARRAY == 0 {
		return core.Bounds{}
	}
	arr := v.ToArray()
	if arr == nil {
		return core.Bounds{}
	}
	defer arr.Release()
	vals := arr.ToValueArray()
	if len(vals) != 4 {
		return core.Bounds{}
	}
	f := func(i int) int {
		if x, ok := vals[i].(float64); ok {
			return int(x)
		}
		return 0
	}
	return core.Bounds{X: f(0), Y: f(1), W: f(2), H: f(3)}
}

// fetchCachedProps reads the fixed property set this backend always
// caches (see automation.createCacheRequest) off of e's local cache.
func fetchCachedProps(e *uiaElement) (cachedProps, error) {
	var p cachedProps
	get := func(propID int32) (ole.VARIANT, error) { return e.getCachedProperty(propID) }

	if v, err := get(propertyRuntimeId); err == nil {
		p.RuntimeID = variantInt32Array(v)
		_ = v.Clear()
	}
	if v, err := get(propertyName); err == nil {
		p.Name = variantString(v)
		_ = v.Clear()
	}
	if v, err := get(propertyAutomationId); err == nil {
		p.AutomationID = variantString(v)
		_ = v.Clear()
	}
	if v, err := get(propertyClassName); err == nil {
		p.ClassName = variantString(v)
		_ = v.Clear()
	}
	if v, err := get(propertyControlType); err == nil {
		p.ControlType = variantInt32(v)
		_ = v.Clear()
	}
	if v, err := get(propertyBoundingRectangle); err == nil {
		p.Bounds = variantBounds(v)
		_ = v.Clear()
	}
	if v, err := get(propertyIsEnabled); err == nil {
		p.Enabled = variantBool(v)
		_ = v.Clear()
	}
	if v, err := get(propertyIsOffscreen); err == nil {
		p.Offscreen = variantBool(v)
		_ = v.Clear()
	}
	if v, err := get(propertyHasKeyboardFocus); err == nil {
		p.KeyboardFocus = variantBool(v)
		_ = v.Clear()
	}
	if v, err := get(propertyProcessId); err == nil {
		p.ProcessID = variantInt32(v)
		_ = v.Clear()
	}
	if len(p.RuntimeID) == 0 {
		return p, core.NewActionFailedError("UIA element carries no RuntimeId (cache request may not have applied)")
	}
	return p, nil
}

// findChildrenBuildCache calls
// IUIAutomationElement::FindAllBuildCache(TreeScope_Children,
// trueCondition, cacheRequest) — the single cross-process hop that
// prefetches a whole tree level's worth of children, each already carrying
// its cached property set, documented in doc.go as this backend's key
// performance lever.
func findChildrenBuildCache(e *uiaElement, trueCond *comObject, cache *cacheRequest) ([]*uiaElement, error) {
	var arrPtr unsafe.Pointer
	hr, _ := e.obj.call(
		idxElementFindAllBuildCache,
		uintptr(treeScopeChildren),
		uintptr(unsafe.Pointer(trueCond)),
		uintptr(unsafe.Pointer(cache.obj)),
		uintptr(unsafe.Pointer(&arrPtr)),
	)
	if hresultOf(hr) != hrOK {
		return nil, mapHRESULT("IUIAutomationElement.FindAllBuildCache", hresultOf(hr), core.ErrActionFailed)
	}
	if arrPtr == nil {
		return nil, nil
	}
	arr := (*comObject)(arrPtr)
	defer arr.Release()

	var length int32
	if hr, _ := arr.call(idxElementArrayGetLength, uintptr(unsafe.Pointer(&length))); hresultOf(hr) != hrOK {
		return nil, mapHRESULT("IUIAutomationElementArray.get_Length", hresultOf(hr), core.ErrActionFailed)
	}
	out := make([]*uiaElement, 0, length)
	for i := int32(0); i < length; i++ {
		var elPtr unsafe.Pointer
		hr, _ := arr.call(idxElementArrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&elPtr)))
		if hresultOf(hr) != hrOK || elPtr == nil {
			continue
		}
		out = append(out, &uiaElement{obj: (*comObject)(elPtr)})
	}
	return out, nil
}

// setFocus calls IUIAutomationElement::SetFocus (slot 3).
func (e *uiaElement) setFocus() error {
	hr, _ := e.obj.call(idxElementSetFocus)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationElement.SetFocus", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

// getCurrentPattern calls IUIAutomationElement::GetCurrentPattern(patternId)
// (slot 13), returning the pattern's COM pointer or nil (with no error) if
// the element does not support it (UIA returns S_OK with a null pointer in
// that case, not a failing HRESULT).
func (e *uiaElement) getCurrentPattern(patternID int32) (*comObject, error) {
	var ptr unsafe.Pointer
	hr, _ := e.obj.call(idxElementGetCurrentPattern, uintptr(patternID), uintptr(unsafe.Pointer(&ptr)))
	if hresultOf(hr) != hrOK {
		return nil, mapHRESULT("IUIAutomationElement.GetCurrentPattern", hresultOf(hr), core.ErrActionFailed)
	}
	if ptr == nil {
		return nil, nil
	}
	return (*comObject)(ptr), nil
}
