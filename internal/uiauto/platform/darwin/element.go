package darwin

import (
	"fmt"
	"math"
	"strconv"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// fixedAttrNames is the fixed attribute set every node fetch requests in
// one AXUIElementCopyMultipleAttributeValues call, per the plan. AXWindows
// only makes sense on an application element and AXParent/AXMain are only
// populated when the element actually carries them; attributes() omits
// whatever the element does not support rather than erroring.
var fixedAttrNames = []string{
	"AXRole", "AXSubrole", "AXTitle", "AXDescription", "AXValue", "AXHelp",
	"AXEnabled", "AXFocused", "AXPosition", "AXSize", "AXChildren",
	"AXParent", "AXWindows", "AXMain", "AXSelected", "AXIdentifier",
}

// Point mirrors a decoded CGPoint (AXPosition).
type Point struct{ X, Y float64 }

// Size mirrors a decoded CGSize (AXSize).
type Size struct{ W, H float64 }

// Native.Data keys. axHandle/pid let refFromElement act on the element
// immediately within the same backend instance; identifier/role/indexPath
// are the durable, pointer-independent re-resolution key the plan asks
// for, since a raw AXUIElementRef handle is not safe to reuse once its
// backend has been Close()d or the element has been recreated.
const (
	nativeHandleKey     = "axHandle" // hex string, e.g. "0x7f8a1c0"
	nativePIDKey        = "pid"
	nativeIdentifierKey = "identifier"
	nativeIndexPathKey  = "indexPath" // []int, root-relative child indices
)

// fetchedNode is everything traverse.go/backend.go need about one
// AXUIElement, gathered from a single batched attributes() call.
type fetchedNode struct {
	ref         axRef
	role        string
	subrole     string
	title       string
	description string
	value       string
	help        string
	enabled     bool
	focused     bool
	main        bool
	selected    bool
	identifier  string
	bounds      core.Bounds
	children    []axRef
	windows     []axRef
}

func formatAXValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// parseFetchedNode decodes a batched attributes() map into a fetchedNode.
func parseFetchedNode(ref axRef, raw map[string]any) *fetchedNode {
	n := &fetchedNode{
		ref:         ref,
		role:        attrString(raw, "AXRole"),
		subrole:     attrString(raw, "AXSubrole"),
		title:       attrString(raw, "AXTitle"),
		description: attrString(raw, "AXDescription"),
		help:        attrString(raw, "AXHelp"),
		enabled:     attrBool(raw, "AXEnabled"),
		focused:     attrBool(raw, "AXFocused"),
		main:        attrBool(raw, "AXMain"),
		selected:    attrBool(raw, "AXSelected"),
		identifier:  attrString(raw, "AXIdentifier"),
		children:    attrRefs(raw, "AXChildren"),
		windows:     attrRefs(raw, "AXWindows"),
	}
	if v, ok := attrValue(raw, "AXValue"); ok {
		n.value = formatAXValue(v)
	}
	var pos Point
	var size Size
	if v, ok := attrValue(raw, "AXPosition"); ok {
		if p, ok := v.(Point); ok {
			pos = p
		}
	}
	if v, ok := attrValue(raw, "AXSize"); ok {
		if s, ok := v.(Size); ok {
			size = s
		}
	}
	n.bounds = core.Bounds{X: int(pos.X), Y: int(pos.Y), W: int(size.W), H: int(size.H)}
	return n
}

// actionsFor derives a coarse Actions list from what this node advertises,
// without an extra actionNames() round trip (Perform fetches actionNames
// itself, on demand, when it needs to pick a concrete AX action name).
func actionsFor(n *fetchedNode) []core.ActionKind {
	var actions []core.ActionKind
	actions = append(actions, core.ActionInvoke, core.ActionFocus)
	if n.subrole == "AXSecureTextField" || n.role == "AXTextField" || n.role == "AXTextArea" {
		actions = append(actions, core.ActionSetValue, core.ActionType)
	}
	if n.selected {
		actions = append(actions, core.ActionSelect)
	}
	return actions
}

// toElement converts a fetchedNode into the normalized core.Element,
// stashing both the live handle (for immediate re-use within this backend
// instance) and the durable (pid, identifier, role, index-path) tuple in
// Native.Data — the escape hatch the plan calls out for a subrole that has
// no cross-platform equivalent, and the re-resolution key for a handle
// that went stale.
func (n *fetchedNode) toElement(backendName string, appPID int32, indexPath []int) *core.Element {
	extras := map[string]any{
		nativeHandleKey: fmt.Sprintf("0x%x", n.ref.Handle),
		nativePIDKey:    int(n.ref.PID),
	}
	if n.identifier != "" {
		extras[nativeIdentifierKey] = n.identifier
	}
	if len(indexPath) > 0 {
		extras[nativeIndexPathKey] = append([]int(nil), indexPath...)
	}
	if n.help != "" {
		extras["help"] = n.help
	}
	if n.selected {
		extras["selected"] = true
	}
	if n.main {
		extras["main"] = true
	}

	el := &core.Element{
		Role:        core.NormalizeRole("ax", n.role),
		Name:        n.title,
		Value:       n.value,
		Description: n.description,
		Bounds:      n.bounds,
		Enabled:     n.enabled,
		Visible:     true, // AX exposes no direct "visible" flag; unreachable/off-screen elements simply are not returned by AXChildren traversal.
		Focused:     n.focused,
		Actions:     actionsFor(n),
		Backend:     backendName,
		AppID:       strconv.Itoa(int(appPID)),
		Native: core.NativeData{
			Platform: "ax",
			Role:     n.role,
			SubRole:  n.subrole,
			Data:     extras,
		},
	}
	return el
}

// refFromElement recovers the axRef an element was built from, from the
// live-handle Native.Data fields toElement populates. A missing/malformed
// handle (e.g. a synthetic root Element, or one built by another backend)
// surfaces as ELEMENT_NOT_FOUND; a handle whose backend has since been
// Close()d is only detectable once an AX call against it returns
// kAXErrorInvalidUIElement, which mapAXError turns into STALE_REF — this
// package does not attempt transparent re-resolution from the durable
// (pid, identifier, indexPath) tuple, that is a documented follow-up (see
// the Phase 5 KB summary's Deferred section).
func refFromElement(el *core.Element) (axRef, error) {
	if el == nil {
		return axRef{}, core.NewInvalidArgsError("nil element")
	}
	if el.Native.Data == nil {
		return axRef{}, core.NewElementNotFoundError("element does not carry an AX handle; re-observe or re-find it")
	}
	hexStr, ok := el.Native.Data[nativeHandleKey].(string)
	if !ok || hexStr == "" {
		return axRef{}, core.NewElementNotFoundError("element does not carry an AX handle; re-observe or re-find it")
	}
	pidAny, ok := el.Native.Data[nativePIDKey]
	if !ok {
		return axRef{}, core.NewElementNotFoundError("element does not carry an owning pid; re-observe or re-find it")
	}
	pid, ok := pidAny.(int)
	if !ok || pid < 0 || pid > math.MaxInt32 {
		return axRef{}, core.NewElementNotFoundError("element carries a malformed pid; re-observe or re-find it")
	}
	// The length check must precede the slice: a handle shorter than the "0x"
	// prefix would otherwise panic instead of being reported as malformed.
	if len(hexStr) < 2 {
		return axRef{}, core.NewInvalidArgsError("element carries a malformed AX handle " + hexStr)
	}
	handle, err := strconv.ParseUint(hexStr[2:], 16, 64)
	if err != nil {
		return axRef{}, core.NewInvalidArgsError("element carries a malformed AX handle " + hexStr)
	}
	return axRef{PID: int32(pid), Handle: uintptr(handle)}, nil
}
