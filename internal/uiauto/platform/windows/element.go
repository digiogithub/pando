package windows

import "github.com/digiogithub/pando/internal/uiauto/core"

// nativeRuntimeIDKey is the core.Element.Native.Data key under which the
// element's encoded UIA RuntimeId (see runtimeid.go) is stashed, mirroring
// the Linux backend's nativeBusKey/nativePathKey pair. It is the durable
// identity used to look an element back up in the backend's live handle
// table (backend_windows.go).
const nativeRuntimeIDKey = "runtimeId"

// cachedProps holds the fixed set of UIA properties this backend always
// pre-fetches for every element it touches, in one batched cache request
// per tree level (see doc.go and traverse.go): Name, ControlType,
// AutomationId, ClassName, BoundingRectangle, IsEnabled, IsOffscreen and
// HasKeyboardFocus, plus the RuntimeId used for identity.
type cachedProps struct {
	RuntimeID     []int32
	Name          string
	AutomationID  string
	ClassName     string
	ControlType   int32
	Bounds        core.Bounds
	Enabled       bool
	Offscreen     bool
	KeyboardFocus bool
	// ProcessID is the owning process id (UIA_ProcessIdPropertyId), used to
	// populate core.AppInfo/WindowInfo.AppID (as a decimal string) and to
	// resolve a process name via processName (process_windows.go).
	ProcessID int32
}

// buildElement converts cachedProps into the normalized core.Element,
// stashing the encoded RuntimeId in Native.Data so Children/Properties/
// Perform can resolve it back to a live COM pointer through the backend's
// handle table without re-searching for it (mirrors the Linux backend's
// fetchedNode.toElement).
func buildElement(backendName, appID, windowID string, p cachedProps) *core.Element {
	encoded := EncodeRuntimeID(p.RuntimeID)
	el := &core.Element{
		Role:    RoleForControlType(p.ControlType),
		Name:    p.Name,
		Bounds:  p.Bounds,
		Enabled: p.Enabled,
		// UIA reports "offscreen" (positive == not visible); the normalized
		// Visible flag is the logical negation.
		Visible: !p.Offscreen,
		Focused: p.KeyboardFocus,
		Backend: backendName,
		AppID:   appID,
		Native: core.NativeData{
			Platform: "uia",
			Role:     ControlTypeName(p.ControlType),
			Data: map[string]any{
				nativeRuntimeIDKey: encoded,
				"automationId":     p.AutomationID,
				"className":        p.ClassName,
				"controlTypeId":    p.ControlType,
			},
		},
	}
	if windowID != "" {
		el.WindowID = windowID
	}
	return el
}

// runtimeIDOf recovers the encoded UIA RuntimeId an Element was built from,
// preferring the Native.Data handle populated by buildElement. It returns
// "" when el carries none (e.g. a synthetic root Element the Manager builds
// straight from a WindowInfo, which has no Native set at all — see
// backend_windows.go's resolveElement for how that case is handled instead,
// by falling back to AppID/WindowID).
func runtimeIDOf(el *core.Element) string {
	if el == nil || el.Native.Data == nil {
		return ""
	}
	if v, ok := el.Native.Data[nativeRuntimeIDKey].(string); ok {
		return v
	}
	return ""
}
