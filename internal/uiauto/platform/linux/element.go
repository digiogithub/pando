package linux

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

// nativeBusKey / nativePathKey are the NativeData.Data keys under which an
// element's accessibleRef is stashed, so Children/Perform can act on an
// already-found element without re-searching for it.
const (
	nativeBusKey  = "busName"
	nativePathKey = "objectPath"
)

// fetchedNode is everything traverse.go and element.go need about one
// AT-SPI object, gathered with a small, fixed number of D-Bus round trips
// (batched where the protocol allows it) rather than one call per
// attribute.
type fetchedNode struct {
	ref         accessibleRef
	name        string
	description string
	roleName    string
	state       []uint32
	interfaces  []string
	childCount  int32
	bounds      core.Bounds
	value       string
}

func hasIface(interfaces []string, short string) bool {
	for _, i := range interfaces {
		if i == short || strings.HasSuffix(i, "."+short) {
			return true
		}
	}
	return false
}

// fetchNode gathers fetchedNode for ref with a bounded set of D-Bus calls:
// one GetAll for the Accessible properties, one GetRoleName, one GetState,
// one GetInterfaces, and (only when the relevant interface is advertised)
// one Component.GetExtents and one Value property read.
func fetchNode(ctx context.Context, conn busConn, ref accessibleRef) (*fetchedNode, error) {
	props, err := conn.getAllProps(ctx, ref.Bus, ref.Path, accessibleIface)
	if err != nil {
		return nil, err
	}
	n := &fetchedNode{ref: ref}
	if v, ok := props["Name"]; ok {
		n.name, _ = v.Value().(string)
	}
	if v, ok := props["Description"]; ok {
		n.description, _ = v.Value().(string)
	}
	if v, ok := props["ChildCount"]; ok {
		switch cc := v.Value().(type) {
		case int32:
			n.childCount = cc
		case uint32:
			n.childCount = int32(cc)
		}
	}

	if body, err := conn.call(ctx, ref.Bus, ref.Path, accessibleIface, "GetRoleName"); err == nil && len(body) > 0 {
		n.roleName, _ = body[0].(string)
	}

	if body, err := conn.call(ctx, ref.Bus, ref.Path, accessibleIface, "GetState"); err == nil && len(body) > 0 {
		n.state = decodeUint32Slice(body[0])
	}

	if body, err := conn.call(ctx, ref.Bus, ref.Path, accessibleIface, "GetInterfaces"); err == nil && len(body) > 0 {
		if ifaces, ok := body[0].([]string); ok {
			n.interfaces = ifaces
		}
	}

	if hasIface(n.interfaces, "Component") {
		// GetExtents' second argument is an ATSPI_CoordType: 0 = SCREEN
		// (global/root-window pixel coordinates), 1 = WINDOW, 2 = PARENT.
		// This backend always requests SCREEN (0) here, which matters for
		// the Wayland parity work (see internal/uiauto/portal and its
		// Wayland routing plan, block W5): AT-SPI2 reports element bounds
		// in the same global coordinate space as the compositor's pointer
		// input, independent of the display protocol (X11 or Wayland) —
		// AT-SPI is not portal-mediated and needs no ScreenCast consent to
		// report correct positions. That is what lets the desktop_click
		// physical-input fallback (core.ActionResolver) hand these bounds'
		// center straight to core.PhysicalInput.Click without any
		// translation: on Wayland, PhysicalInput's own coordinate space is
		// a ScreenCast stream's position+size rectangle (also expressed in
		// the compositor's global pixel space, per the XDG
		// RemoteDesktop/ScreenCast portal spec), so AT-SPI SCREEN bounds
		// and a ScreenCast stream's rectangle are directly comparable: a
		// point is "in stream S" simply by checking S.Contains(x, y) against
		// these same global coordinates (see portal.Stream.Contains). If
		// this backend ever requested WINDOW or PARENT coordinates instead,
		// the physical-input fallback would silently click the wrong pixel
		// on any window not positioned at the stream's origin.
		if body, err := conn.call(ctx, ref.Bus, ref.Path, componentIface, "GetExtents", uint32(0)); err == nil && len(body) == 4 {
			x, _ := toInt(body[0])
			y, _ := toInt(body[1])
			w, _ := toInt(body[2])
			h, _ := toInt(body[3])
			n.bounds = core.Bounds{X: x, Y: y, W: w, H: h}
		}
	}

	if hasIface(n.interfaces, "Value") {
		if vprops, err := conn.getAllProps(ctx, ref.Bus, ref.Path, valueIface); err == nil {
			if v, ok := vprops["CurrentValue"]; ok {
				n.value = formatValue(v.Value())
			}
		}
	}

	return n, nil
}

// decodeUint32Slice normalizes the several shapes godbus may hand back for
// an "au" (array of uint32) reply body element.
func decodeUint32Slice(v interface{}) []uint32 {
	switch t := v.(type) {
	case []uint32:
		return t
	case []interface{}:
		out := make([]uint32, 0, len(t))
		for _, e := range t {
			if n, ok := toInt(e); ok {
				out = append(out, uint32(n))
			}
		}
		return out
	default:
		return nil
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int32:
		return int(n), true
	case uint32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func formatValue(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	case string:
		return n
	default:
		return fmt.Sprintf("%v", n)
	}
}

// actionsFor derives a coarse, cheap-to-compute Actions list from the
// interfaces/state a node advertises, without an extra round trip to list
// individual AT-SPI actions by name (that is only done in Perform, on
// demand).
func actionsFor(n *fetchedNode, ds decodedState) []core.ActionKind {
	var actions []core.ActionKind
	if hasIface(n.interfaces, "Action") {
		actions = append(actions, core.ActionInvoke)
	}
	if hasIface(n.interfaces, "Component") {
		actions = append(actions, core.ActionFocus)
	}
	if hasIface(n.interfaces, "EditableText") {
		actions = append(actions, core.ActionSetValue, core.ActionType)
	}
	if ds.Checkable || ds.Checked {
		actions = append(actions, core.ActionToggle)
	}
	if ds.Selectable {
		actions = append(actions, core.ActionSelect)
	}
	if ds.Expandable {
		actions = append(actions, core.ActionExpand, core.ActionCollapse)
	}
	return actions
}

// toElement converts a fetchedNode into the normalized core.Element,
// stashing the accessibleRef and raw AT-SPI attributes in Native so a
// caller can act on the element later without re-resolving it.
func (n *fetchedNode) toElement(backendName, appBus string) *core.Element {
	ds := decodeState(n.state)
	extras := ds.nativeExtras()
	extras[nativeBusKey] = n.ref.Bus
	extras[nativePathKey] = string(n.ref.Path)
	extras["interfaces"] = append([]string(nil), n.interfaces...)
	extras["childCount"] = n.childCount

	el := &core.Element{
		Role:        core.NormalizeRole("atspi", n.roleName),
		Name:        n.name,
		Value:       n.value,
		Description: n.description,
		Bounds:      n.bounds,
		Enabled:     ds.elementEnabled(),
		Visible:     ds.elementVisible(),
		Focused:     ds.Focused,
		Actions:     actionsFor(n, ds),
		Backend:     backendName,
		AppID:       appBus,
		Native: core.NativeData{
			Platform: "atspi",
			Role:     n.roleName,
			Data:     extras,
		},
	}
	return el
}

// refFromElement recovers the accessibleRef an element was built from,
// preferring the Native.Data handle populated by toElement, and falling
// back to AppID (bus name) + WindowID (object path) for the synthetic root
// Element the Manager builds from WindowInfo (which has no Native set).
func refFromElement(el *core.Element) (accessibleRef, error) {
	if el == nil {
		return accessibleRef{}, core.NewInvalidArgsError("nil element")
	}
	if el.Native.Data != nil {
		bus, busOK := el.Native.Data[nativeBusKey].(string)
		path, pathOK := el.Native.Data[nativePathKey].(string)
		if busOK && pathOK && bus != "" && path != "" {
			return accessibleRef{Bus: bus, Path: dbus.ObjectPath(path)}, nil
		}
	}
	if el.AppID != "" && el.WindowID != "" {
		return accessibleRef{Bus: el.AppID, Path: dbus.ObjectPath(el.WindowID)}, nil
	}
	return accessibleRef{}, core.NewElementNotFoundError("element does not carry an AT-SPI object handle (busName/objectPath); re-observe or re-find it")
}
