package darwin

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// actionNameHints maps an ActionKind to the AX action names
// (AXUIElementCopyActionNames) that most commonly implement it, per the
// plan's fixed vocabulary (AXPress, AXIncrement, AXDecrement, AXShowMenu,
// AXConfirm, AXCancel). The first advertised action name in this list is
// invoked; invoke additionally falls back to whatever action is first in
// the advertised list (many controls expose a single, always-primary
// action) when none of its hints match.
var actionNameHints = map[core.ActionKind][]string{
	core.ActionInvoke:   {"AXPress", "AXConfirm"},
	core.ActionToggle:   {"AXPress"},
	core.ActionSelect:   {"AXPress"},
	core.ActionExpand:   {"AXShowMenu", "AXPress"},
	core.ActionCollapse: {"AXCancel", "AXPress"},
}

// performAction executes action against ref, following the mapping the
// plan documents: invoke -> AXPress; focus -> set AXFocused true;
// setvalue/type -> set AXValue; toggle/select/expand/collapse -> the
// matching advertised action. scroll/press are not AX actions and always
// return PLATFORM_NOT_SUPPORTED so core.ActionResolver's physical fallback
// takes over. Anything unsupported/missing returns ACTION_FAILED.
func performAction(ctx context.Context, conn axConn, ref axRef, action core.Action) error {
	switch action.Kind {
	case core.ActionFocus:
		return conn.setAttribute(ctx, ref, "AXFocused", true)
	case core.ActionSetValue, core.ActionType:
		return conn.setAttribute(ctx, ref, "AXValue", action.Text)
	case core.ActionInvoke, core.ActionToggle, core.ActionSelect, core.ActionExpand, core.ActionCollapse:
		return performNamedAction(ctx, conn, ref, action.Kind)
	default:
		return core.NewPlatformNotSupportedError("ax backend does not implement action kind " + string(action.Kind))
	}
}

func performNamedAction(ctx context.Context, conn axConn, ref axRef, kind core.ActionKind) error {
	names, err := conn.actionNames(ctx, ref)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return core.NewActionFailedError("element advertises no AX actions")
	}
	for _, hint := range actionNameHints[kind] {
		for _, name := range names {
			if strings.EqualFold(name, hint) {
				return conn.performAction(ctx, ref, name)
			}
		}
	}
	if kind == core.ActionInvoke {
		// Last resort: many widgets expose a single, unnamed-ish primary
		// action at index 0.
		return conn.performAction(ctx, ref, names[0])
	}
	return core.NewActionFailedError("element does not advertise a matching AX action for " + string(kind))
}
