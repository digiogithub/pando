package linux

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// actionNameHints maps an ActionKind to the AT-SPI Action-interface action
// names (as advertised by Action.GetName(i), lowercased) commonly used to
// perform it. The first advertised action whose name contains any of these
// substrings is invoked; when none match, index 0 is used as a last resort
// for ActionInvoke only (many widgets expose a single, unnamed-ish primary
// action at index 0).
var actionNameHints = map[core.ActionKind][]string{
	core.ActionInvoke:   {"click", "press", "activate", "invoke"},
	core.ActionToggle:   {"toggle", "check", "click"},
	core.ActionSelect:   {"select"},
	core.ActionExpand:   {"expand"},
	core.ActionCollapse: {"collapse"},
}

// performAction executes action against ref over conn, following the
// mapping documented in the Phase 2 plan: invoke/toggle/select/expand/
// collapse go through the Action interface, focus through Component,
// setvalue/type through EditableText. Anything an interface does not
// support returns ACTION_FAILED, so core.ActionResolver's native-first,
// physical-fallback policy can take over.
func performAction(ctx context.Context, conn busConn, ref accessibleRef, action core.Action) error {
	switch action.Kind {
	case core.ActionFocus:
		return performFocus(ctx, conn, ref)
	case core.ActionSetValue, core.ActionType:
		return performSetText(ctx, conn, ref, action.Text)
	case core.ActionInvoke, core.ActionToggle, core.ActionSelect, core.ActionExpand, core.ActionCollapse:
		return performNamedAction(ctx, conn, ref, action.Kind)
	default:
		return core.NewPlatformNotSupportedError("atspi backend does not implement action kind " + string(action.Kind))
	}
}

func performFocus(ctx context.Context, conn busConn, ref accessibleRef) error {
	body, err := conn.call(ctx, ref.Bus, ref.Path, componentIface, "GrabFocus")
	if err != nil {
		return core.NewActionFailedError("Component.GrabFocus failed: " + err.Error())
	}
	if ok, isBool := boolResult(body); isBool && !ok {
		return core.NewActionFailedError("Component.GrabFocus returned false")
	}
	return nil
}

func performSetText(ctx context.Context, conn busConn, ref accessibleRef, text string) error {
	body, err := conn.call(ctx, ref.Bus, ref.Path, editTextIface, "SetTextContents", text)
	if err != nil {
		return core.NewActionFailedError("EditableText.SetTextContents failed: " + err.Error())
	}
	if ok, isBool := boolResult(body); isBool && !ok {
		return core.NewActionFailedError("EditableText.SetTextContents returned false")
	}
	return nil
}

func performNamedAction(ctx context.Context, conn busConn, ref accessibleRef, kind core.ActionKind) error {
	names, err := actionNames(ctx, conn, ref)
	if err != nil {
		return core.NewActionFailedError("could not list AT-SPI actions: " + err.Error())
	}
	if len(names) == 0 {
		return core.NewActionFailedError("element does not implement the AT-SPI Action interface")
	}

	idx := -1
	for _, hint := range actionNameHints[kind] {
		for i, name := range names {
			if strings.Contains(strings.ToLower(name), hint) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			break
		}
	}
	if idx < 0 {
		if kind == core.ActionInvoke {
			idx = 0
		} else {
			return core.NewActionFailedError("no AT-SPI action named like " + string(kind) + " is advertised; available: " + strings.Join(names, ", "))
		}
	}

	body, err := conn.call(ctx, ref.Bus, ref.Path, actionIface, "DoAction", int32(idx))
	if err != nil {
		return core.NewActionFailedError("Action.DoAction failed: " + err.Error())
	}
	if ok, isBool := boolResult(body); isBool && !ok {
		return core.NewActionFailedError("Action.DoAction returned false")
	}
	return nil
}

func actionNames(ctx context.Context, conn busConn, ref accessibleRef) ([]string, error) {
	props, err := conn.getAllProps(ctx, ref.Bus, ref.Path, actionIface)
	if err != nil {
		return nil, err
	}
	n := 0
	if v, ok := props["NActions"]; ok {
		if i, ok := toInt(v.Value()); ok {
			n = i
		}
	}
	if n == 0 {
		if body, err := conn.call(ctx, ref.Bus, ref.Path, actionIface, "GetNActions"); err == nil && len(body) > 0 {
			if i, ok := toInt(body[0]); ok {
				n = i
			}
		}
	}
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		body, err := conn.call(ctx, ref.Bus, ref.Path, actionIface, "GetName", int32(i))
		if err != nil || len(body) == 0 {
			names = append(names, "")
			continue
		}
		name, _ := body[0].(string)
		names = append(names, name)
	}
	return names, nil
}

func boolResult(body []interface{}) (value bool, isBool bool) {
	if len(body) == 0 {
		return false, false
	}
	b, ok := body[0].(bool)
	return b, ok
}
