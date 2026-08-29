//go:build windows

package windows

import (
	"unsafe"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Pattern vtable slot indices (UIAutomationClient.idl). Slots 0-2 are
// IUnknown on every pattern interface.
const (
	idxInvokeInvoke = 3

	idxValueSetValue = 3

	idxToggleToggle = 3

	idxSelectionItemSelect = 3

	idxExpandCollapseExpand   = 3
	idxExpandCollapseCollapse = 4

	idxScrollItemScrollIntoView = 3

	idxLegacyIAccessibleSelect          = 3
	idxLegacyIAccessibleDoDefaultAction = 4
	idxLegacyIAccessibleSetValue        = 5
)

// performAction executes action against e, following the mapping the
// Phase 4 plan documents: invoke -> InvokePattern (falling back to
// LegacyIAccessible::DoDefaultAction), setvalue/type -> ValuePattern,
// toggle -> TogglePattern, select -> SelectionItemPattern, expand/collapse
// -> ExpandCollapsePattern, scroll -> ScrollItemPattern, focus ->
// SetFocus. Anything the target does not support (GetCurrentPattern
// returning a null pointer, or the pattern method itself failing) returns
// ACTION_FAILED so core.ActionResolver's native-first, physical-fallback
// policy (wired by Phase 3's PhysicalInput) can take over — mirroring the
// Linux AT-SPI backend's actions.go exactly.
func performAction(e *uiaElement, action core.Action) error {
	switch action.Kind {
	case core.ActionFocus:
		return e.setFocus()
	case core.ActionSetValue, core.ActionType:
		return performSetValue(e, action.Text)
	case core.ActionInvoke:
		return performInvoke(e)
	case core.ActionToggle:
		return performToggle(e)
	case core.ActionSelect:
		return performSelect(e)
	case core.ActionExpand:
		return performExpandCollapse(e, true)
	case core.ActionCollapse:
		return performExpandCollapse(e, false)
	case core.ActionScroll:
		return performScrollIntoView(e)
	default:
		return core.NewPlatformNotSupportedError("uia backend does not implement action kind " + string(action.Kind))
	}
}

func performInvoke(e *uiaElement) error {
	pat, err := e.getCurrentPattern(patternInvoke)
	if err != nil {
		return err
	}
	if pat != nil {
		defer pat.Release()
		hr, _ := pat.call(idxInvokeInvoke)
		if hresultOf(hr) == hrOK {
			return nil
		}
		// fall through to LegacyIAccessible as a last resort, matching the
		// AT-SPI backend's "no advertised action -> ACTION_FAILED" shape.
	}
	legacy, err := e.getCurrentPattern(patternLegacyIAccessible)
	if err != nil {
		return err
	}
	if legacy == nil {
		return core.NewActionFailedError("element supports neither InvokePattern nor LegacyIAccessiblePattern")
	}
	defer legacy.Release()
	hr, _ := legacy.call(idxLegacyIAccessibleDoDefaultAction)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationLegacyIAccessiblePattern.DoDefaultAction", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

func performSetValue(e *uiaElement, text string) error {
	pat, err := e.getCurrentPattern(patternValue)
	if err != nil {
		return err
	}
	if pat == nil {
		legacy, lerr := e.getCurrentPattern(patternLegacyIAccessible)
		if lerr != nil {
			return lerr
		}
		if legacy == nil {
			return core.NewActionFailedError("element supports neither ValuePattern nor LegacyIAccessiblePattern")
		}
		defer legacy.Release()
		b := stringToBSTR(text)
		defer freeBSTR(b)
		hr, _ := legacy.call(idxLegacyIAccessibleSetValue, uintptr(unsafe.Pointer(b)))
		if hresultOf(hr) != hrOK {
			return mapHRESULT("IUIAutomationLegacyIAccessiblePattern.SetValue", hresultOf(hr), core.ErrActionFailed)
		}
		return nil
	}
	defer pat.Release()
	b := stringToBSTR(text)
	defer freeBSTR(b)
	hr, _ := pat.call(idxValueSetValue, uintptr(unsafe.Pointer(b)))
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationValuePattern.SetValue", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

func performToggle(e *uiaElement) error {
	pat, err := e.getCurrentPattern(patternToggle)
	if err != nil {
		return err
	}
	if pat == nil {
		return core.NewActionFailedError("element does not support TogglePattern")
	}
	defer pat.Release()
	hr, _ := pat.call(idxToggleToggle)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationTogglePattern.Toggle", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

func performSelect(e *uiaElement) error {
	pat, err := e.getCurrentPattern(patternSelectionItem)
	if err != nil {
		return err
	}
	if pat == nil {
		return core.NewActionFailedError("element does not support SelectionItemPattern")
	}
	defer pat.Release()
	hr, _ := pat.call(idxSelectionItemSelect)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationSelectionItemPattern.Select", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

func performExpandCollapse(e *uiaElement, expand bool) error {
	pat, err := e.getCurrentPattern(patternExpandCollapse)
	if err != nil {
		return err
	}
	if pat == nil {
		return core.NewActionFailedError("element does not support ExpandCollapsePattern")
	}
	defer pat.Release()
	idx := idxExpandCollapseCollapse
	name := "Collapse"
	if expand {
		idx = idxExpandCollapseExpand
		name = "Expand"
	}
	hr, _ := pat.call(idx)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationExpandCollapsePattern."+name, hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}

func performScrollIntoView(e *uiaElement) error {
	pat, err := e.getCurrentPattern(patternScrollItem)
	if err != nil {
		return err
	}
	if pat == nil {
		return core.NewActionFailedError("element does not support ScrollItemPattern")
	}
	defer pat.Release()
	hr, _ := pat.call(idxScrollItemScrollIntoView)
	if hresultOf(hr) != hrOK {
		return mapHRESULT("IUIAutomationScrollItemPattern.ScrollIntoView", hresultOf(hr), core.ErrActionFailed)
	}
	return nil
}
