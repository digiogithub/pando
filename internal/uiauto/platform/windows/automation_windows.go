//go:build windows

package windows

import (
	"fmt"
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
	ole "github.com/go-ole/go-ole"
)

// automation wraps the IUIAutomation COM object (CUIAutomation, or
// CUIAutomation8 when available) and exposes the small subset of its vtable
// this backend needs. Every method here must only ever be called from the
// comWorker's dedicated OS thread (see worker_windows.go); backend_windows.go
// is the only caller and always goes through comWorker.run.
type automation struct {
	obj *comObject
}

// IUIAutomation vtable slot indices, from UIAutomationClient.idl. Slots 0-2
// are the shared IUnknown QueryInterface/AddRef/Release. This backend is
// compile-verified only (GOOS=windows go build), never exercised against a
// real COM implementation — these indices are transcribed from the public
// IDL/headers as accurately as possible, and are the single highest-risk
// spot for silent drift in this package if that transcription is off.
const (
	idxCompareElements              = 3
	idxCompareRuntimeIds            = 4
	idxGetRootElement               = 5
	idxElementFromHandle            = 6
	idxElementFromPoint             = 7
	idxGetFocusedElement            = 8
	idxGetRootElementBuildCache     = 9
	idxElementFromHandleBuildCache  = 10
	idxElementFromPointBuildCache   = 11
	idxGetFocusedElementBuildCache  = 12
	idxCreateTreeWalker             = 13
	idxGetControlViewWalker         = 14
	idxGetContentViewWalker         = 15
	idxGetRawViewWalker             = 16
	idxGetRawViewCondition          = 17
	idxGetControlViewCondition      = 18
	idxGetContentViewCondition      = 19
	idxCreateCacheRequest           = 20
	idxCreateTrueCondition          = 21
	idxCreateFalseCondition         = 22
	idxCreatePropertyCondition      = 23
	idxCreatePropertyConditionEx    = 24
	idxCreateAndCondition           = 25
	idxCreateAndConditionFromArray  = 26
	idxCreateAndConditionFromNative = 27
	idxCreateOrCondition            = 28
)

// newAutomation constructs the IUIAutomation COM object, preferring
// CUIAutomation8 (Windows 8+) and falling back to the original
// CUIAutomation. It must run on the comWorker thread.
func newAutomation() (*automation, error) {
	if unk, err := ole.CreateInstance(clsidCUIAutomation8, iidIUIAutomation); err == nil {
		return &automation{obj: asComObject(unk)}, nil
	}
	unk, err := ole.CreateInstance(clsidCUIAutomation, iidIUIAutomation)
	if err != nil {
		return nil, fmt.Errorf("uia: could not create the CUIAutomation COM object (CoCreateInstance failed for both CUIAutomation8 and CUIAutomation): %w", err)
	}
	return &automation{obj: asComObject(unk)}, nil
}

func (a *automation) release() {
	if a == nil || a.obj == nil {
		return
	}
	a.obj.Release()
}

// callOutPtr invokes vtable slot index with args, expecting the method's
// final [out, retval] parameter to be a single COM interface pointer. It
// returns that pointer (nil on failure) and the raw HRESULT.
func (a *automation) callOutPtr(index int, args ...uintptr) (unsafe.Pointer, uint32) {
	var out unsafe.Pointer
	full := append(append([]uintptr(nil), args...), uintptr(unsafe.Pointer(&out)))
	hr, _ := a.obj.call(index, full...)
	return out, hresultOf(hr)
}

// getRootElement calls IUIAutomation::GetRootElement (slot 5), returning
// the desktop root element.
func (a *automation) getRootElement() (*comObject, error) {
	ptr, hr := a.callOutPtr(idxGetRootElement)
	if hr != hrOK || ptr == nil {
		return nil, mapHRESULT("IUIAutomation.GetRootElement", hr, core.ErrActionFailed)
	}
	return (*comObject)(ptr), nil
}

// createTrueCondition calls IUIAutomation::CreateTrueCondition (slot 21).
func (a *automation) createTrueCondition() (*comObject, error) {
	ptr, hr := a.callOutPtr(idxCreateTrueCondition)
	if hr != hrOK || ptr == nil {
		return nil, mapHRESULT("IUIAutomation.CreateTrueCondition", hr, core.ErrActionFailed)
	}
	return (*comObject)(ptr), nil
}

// createCacheRequest calls IUIAutomation::CreateCacheRequest (slot 20) and
// configures it (via cacheRequest_windows.go's addProperty/setScope) to
// pre-fetch the fixed property set this backend always wants: Name,
// ControlType, AutomationId, ClassName, BoundingRectangle, IsEnabled,
// IsOffscreen, HasKeyboardFocus and RuntimeId, over TreeScope_Subtree —
// this is the one-cross-process-hop-per-level batching documented in
// doc.go.
func (a *automation) createCacheRequest() (*cacheRequest, error) {
	ptr, hr := a.callOutPtr(idxCreateCacheRequest)
	if hr != hrOK || ptr == nil {
		return nil, mapHRESULT("IUIAutomation.CreateCacheRequest", hr, core.ErrActionFailed)
	}
	cr := &cacheRequest{obj: (*comObject)(ptr)}
	for _, prop := range []int32{
		propertyName, propertyControlType, propertyAutomationId, propertyClassName,
		propertyBoundingRectangle, propertyIsEnabled, propertyIsOffscreen,
		propertyHasKeyboardFocus, propertyRuntimeId, propertyProcessId,
	} {
		if err := cr.addProperty(prop); err != nil {
			cr.release()
			return nil, err
		}
	}
	if err := cr.setTreeScope(treeScopeSubtree); err != nil {
		cr.release()
		return nil, err
	}
	return cr, nil
}
