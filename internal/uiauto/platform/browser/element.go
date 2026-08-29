package browser

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// AppID is the fixed identifier of the single virtual "application" the CDP
// backend reports: the connected browser itself (see Apps/Windows in
// backend.go). Individual pages are windows within it, keyed by CDP target
// id. Exported so internal/uiauto.Manager can recognize a browser-scoped
// operation (app_id == AppID) and route it to this backend without ever
// having to probe/launch a browser to find out.
const AppID = "browser"

// appID is the unexported, package-local alias used throughout this file
// and backend.go so the routing-only exported name above does not force a
// rename of every call site.
const appID = AppID

// nativeTargetIDKey / nativeAXNodeIDKey / nativeBackendIDKey are the
// NativeData.Data keys under which an element's CDP identity is stashed --
// the durable key (backendDOMNodeId, kept alongside the current AX nodeId)
// so Children/Perform/Properties can act on an already-found element
// without re-searching for it.
const (
	nativeTargetIDKey  = "targetId"
	nativeAXNodeIDKey  = "axNodeId"
	nativeBackendIDKey = "backendDOMNodeId"
)

// axValue decodes an accessibility.Value's raw JSON payload into a plain Go
// value (string, float64, bool, or nil).
func axValue(v *accessibility.Value) (any, bool) {
	if v == nil || len(v.Value) == 0 {
		return nil, false
	}
	var out any
	if err := json.Unmarshal(v.Value, &out); err != nil {
		return nil, false
	}
	return out, true
}

// axString renders an accessibility.Value as a string, regardless of its
// underlying JSON type.
func axString(v *accessibility.Value) string {
	val, ok := axValue(v)
	if !ok {
		return ""
	}
	switch t := val.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// axBool coerces a decoded AX property value (bool, or the string "true"/
// "false" some tristate-ish properties use) into a bool.
func axBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(t) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// invokableRoles / editableRoles drive the coarse, zero-extra-call Actions
// list a normalized Element carries, mirroring the heuristic the AT-SPI2
// (Phase 2) and AXUIElement (Phase 5) backends use.
var invokableRoles = map[core.Role]bool{
	core.RoleButton:   true,
	core.RoleLink:     true,
	core.RoleMenuItem: true,
	core.RoleTab:      true,
	core.RoleCheckbox: true,
	core.RoleRadio:    true,
}

var editableRoles = map[core.Role]bool{
	core.RoleTextField: true,
	core.RoleTextArea:  true,
	core.RoleComboBox:  true,
}

// actionsForRole derives the Actions list for role from its canonical
// vocabulary plus the extra AX properties already decoded into extras (no
// additional round trip).
func actionsForRole(role core.Role, focusable bool, extras map[string]any) []core.ActionKind {
	var actions []core.ActionKind
	if focusable {
		actions = append(actions, core.ActionFocus)
	}
	if invokableRoles[role] {
		actions = append(actions, core.ActionInvoke)
	}
	if editableRoles[role] {
		actions = append(actions, core.ActionSetValue, core.ActionType)
	}
	if _, ok := extras[string(accessibility.PropertyNameChecked)]; ok {
		actions = append(actions, core.ActionToggle)
	}
	if _, ok := extras[string(accessibility.PropertyNameExpanded)]; ok {
		actions = append(actions, core.ActionExpand, core.ActionCollapse)
	}
	actions = append(actions, core.ActionScroll)
	return actions
}

// toElement converts a CDP accessibility.Node into the normalized
// core.Element, stashing the target/AX/backend-DOM identity in Native so a
// caller can act on the element later without re-resolving it. Bounds are
// deliberately left empty here (see backend.go's Perform/Properties):
// fetching dom.GetBoxModel for every node during traversal would defeat the
// point of incremental descent.
func toElement(n *accessibility.Node, targetID target.ID) *core.Element {
	rawRole := axString(n.Role)
	role := core.NormalizeRole("cdp", rawRole)

	extras := make(map[string]any, len(n.Properties)+3)
	enabled := true
	visible := !n.Ignored
	var focused, focusable bool

	for _, p := range n.Properties {
		if p == nil || p.Value == nil {
			continue
		}
		raw, ok := axValue(p.Value)
		if !ok {
			continue
		}
		switch p.Name {
		case accessibility.PropertyNameDisabled:
			if b, ok := axBool(raw); ok {
				enabled = !b
			}
		case accessibility.PropertyNameHidden:
			if b, ok := axBool(raw); ok && b {
				visible = false
			}
		case accessibility.PropertyNameFocused:
			if b, ok := axBool(raw); ok {
				focused = b
			}
		case accessibility.PropertyNameFocusable:
			if b, ok := axBool(raw); ok {
				focusable = b
			}
		default:
			extras[string(p.Name)] = raw
		}
	}

	extras[nativeTargetIDKey] = string(targetID)
	extras[nativeAXNodeIDKey] = string(n.NodeID)
	if n.BackendDOMNodeID != 0 {
		extras[nativeBackendIDKey] = int64(n.BackendDOMNodeID)
	}
	if n.Ignored {
		extras["ignored"] = true
	}
	if rawChrome := axString(n.ChromeRole); rawChrome != "" {
		extras["chromeRole"] = rawChrome
	}

	return &core.Element{
		Role:        role,
		Name:        axString(n.Name),
		Value:       axString(n.Value),
		Description: axString(n.Description),
		Enabled:     enabled,
		Visible:     visible,
		Focused:     focused,
		Actions:     actionsForRole(role, focusable, extras),
		Backend:     "cdp",
		AppID:       appID,
		Native: core.NativeData{
			Platform: "cdp",
			Role:     rawRole,
			Data:     extras,
		},
	}
}

// refFromElement recovers the CDP (targetID, axNodeID) an element was built
// from, preferring the Native.Data handle populated by toElement, and
// falling back to WindowID (the synthetic root Element the Manager builds
// from WindowInfo, which carries no Native at all -- see
// Manager.rootElement). hasAXNodeID is false only for that synthetic root
// fallback, telling the caller to fetch the target's root AX node first.
func refFromElement(el *core.Element) (targetID target.ID, axNodeID accessibility.NodeID, hasAXNodeID bool, err error) {
	if el == nil {
		return "", "", false, core.NewInvalidArgsError("nil element")
	}
	if el.Native.Data != nil {
		if t, ok := el.Native.Data[nativeTargetIDKey].(string); ok && t != "" {
			targetID = target.ID(t)
		}
		if a, ok := el.Native.Data[nativeAXNodeIDKey].(string); ok && a != "" {
			axNodeID = accessibility.NodeID(a)
			hasAXNodeID = true
		}
	}
	if targetID == "" && el.WindowID != "" {
		targetID = target.ID(el.WindowID)
	}
	if targetID == "" {
		return "", "", false, core.NewElementNotFoundError("element does not carry a CDP target id; re-observe or re-find it")
	}
	return targetID, axNodeID, hasAXNodeID, nil
}

// backendNodeIDFromElement recovers the durable DOM backend node id an
// element was built from, if any (absent for AX nodes with no backing DOM
// node, e.g. some ignored/virtual nodes).
func backendNodeIDFromElement(el *core.Element) (cdp.BackendNodeID, bool) {
	if el == nil || el.Native.Data == nil {
		return 0, false
	}
	switch v := el.Native.Data[nativeBackendIDKey].(type) {
	case int64:
		return cdp.BackendNodeID(v), true
	case float64:
		return cdp.BackendNodeID(v), true
	default:
		return 0, false
	}
}
