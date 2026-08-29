package core

import "context"

// ActionKind enumerates the semantic actions that can be performed on an
// Element, either natively (through the accessibility backend) or, as a
// fallback, physically (through synthetic mouse/keyboard input).
type ActionKind string

const (
	ActionInvoke   ActionKind = "invoke"
	ActionFocus    ActionKind = "focus"
	ActionSetValue ActionKind = "setvalue"
	ActionToggle   ActionKind = "toggle"
	ActionSelect   ActionKind = "select"
	ActionExpand   ActionKind = "expand"
	ActionCollapse ActionKind = "collapse"
	ActionScroll   ActionKind = "scroll"
	ActionPress    ActionKind = "press"
	ActionType     ActionKind = "type"
)

// Action is a single request to perform ActionKind Kind against an Element,
// carrying whatever payload that kind needs.
type Action struct {
	Kind ActionKind
	// Text carries the payload for ActionSetValue/ActionType.
	Text string
	// Amount carries the scroll delta for ActionScroll (positive/negative,
	// backend-defined units).
	Amount int
	// Key carries the key or chord identifier for ActionPress (e.g.
	// "Enter", "Ctrl+A").
	Key string
}

// PhysicalInput is the minimal synthetic-input surface an ActionResolver
// falls back to when a backend cannot perform an action natively. It is
// implemented by the internal/uiauto/input package in later phases; core
// only depends on this interface.
type PhysicalInput interface {
	Click(x, y int) error
	MoveMouse(x, y int) error
	TypeText(s string) error
	PressKey(key string) error
	Scroll(x, y, amount int) error
}

// ActionResult reports how an action was ultimately carried out.
type ActionResult struct {
	// Method is "native" when the backend performed the action directly,
	// or "physical" when a synthetic-input fallback was used.
	Method string
	Action Action
	// Notes records anything worth surfacing to the caller/LLM, e.g. why a
	// fallback was taken.
	Notes []string
}

// ActionResolver implements the native-first, physical-fallback policy: it
// always tries the accessibility Backend first, and only falls back to
// PhysicalInput when the backend fails or does not support the action, and
// AllowPhysical is true.
type ActionResolver struct {
	Backend       Backend
	Physical      PhysicalInput
	AllowPhysical bool
}

// NewActionResolver builds an ActionResolver.
func NewActionResolver(backend Backend, physical PhysicalInput, allowPhysical bool) *ActionResolver {
	return &ActionResolver{Backend: backend, Physical: physical, AllowPhysical: allowPhysical}
}

// canFallback reports whether a physical fallback is usable for el: it
// requires AllowPhysical, a configured PhysicalInput and non-empty bounds.
func (r *ActionResolver) canFallback(el *Element) bool {
	return r.AllowPhysical && r.Physical != nil && el != nil && !el.Bounds.Empty()
}

// isUnsupportedOrFailed reports whether err represents a native-action
// failure that should trigger the physical fallback (ACTION_FAILED or
// PLATFORM_NOT_SUPPORTED). Any other error (e.g. context cancellation) is
// propagated as-is.
func isUnsupportedOrFailed(err error) bool {
	de, ok := AsDesktopError(err)
	if !ok {
		return false
	}
	return de.Code == ErrActionFailed || de.Code == ErrPlatformNotSupported
}

// Click performs a click: native Invoke first, physical click at
// Bounds.Center() as fallback.
func (r *ActionResolver) Click(ctx context.Context, el *Element) (*ActionResult, error) {
	act := Action{Kind: ActionInvoke}
	err := r.Backend.Perform(ctx, el, act)
	if err == nil {
		return &ActionResult{Method: "native", Action: act}, nil
	}
	if !isUnsupportedOrFailed(err) || !r.canFallback(el) {
		return nil, err
	}
	x, y := el.Bounds.Center()
	if perr := r.Physical.Click(x, y); perr != nil {
		return nil, NewActionFailedError("native invoke and physical click both failed: " + perr.Error())
	}
	return &ActionResult{
		Method: "physical",
		Action: act,
		Notes:  []string{"native invoke failed (" + err.Error() + "); fell back to physical click at element bounds center"},
	}, nil
}

// Type enters text: native SetValue, then native Type, then
// physical focus+TypeText as fallback.
func (r *ActionResolver) Type(ctx context.Context, el *Element, text string) (*ActionResult, error) {
	setAct := Action{Kind: ActionSetValue, Text: text}
	if err := r.Backend.Perform(ctx, el, setAct); err == nil {
		return &ActionResult{Method: "native", Action: setAct}, nil
	} else if !isUnsupportedOrFailed(err) {
		return nil, err
	}

	typeAct := Action{Kind: ActionType, Text: text}
	err := r.Backend.Perform(ctx, el, typeAct)
	if err == nil {
		return &ActionResult{Method: "native", Action: typeAct}, nil
	}
	if !isUnsupportedOrFailed(err) || !r.canFallback(el) {
		return nil, err
	}

	if ferr := r.Backend.Perform(ctx, el, Action{Kind: ActionFocus}); ferr != nil && !isUnsupportedOrFailed(ferr) {
		return nil, ferr
	} else if ferr != nil && r.canFallback(el) {
		x, y := el.Bounds.Center()
		if cerr := r.Physical.Click(x, y); cerr != nil {
			return nil, NewActionFailedError("could not focus element for physical typing: " + cerr.Error())
		}
	}
	if perr := r.Physical.TypeText(text); perr != nil {
		return nil, NewActionFailedError("native setvalue/type and physical typing both failed: " + perr.Error())
	}
	return &ActionResult{
		Method: "physical",
		Action: typeAct,
		Notes:  []string{"native setvalue/type failed (" + err.Error() + "); fell back to physical focus+type"},
	}, nil
}

// Scroll scrolls el (or the point at its bounds center): native Scroll
// first, physical scroll as fallback.
func (r *ActionResolver) Scroll(ctx context.Context, el *Element, amount int) (*ActionResult, error) {
	act := Action{Kind: ActionScroll, Amount: amount}
	err := r.Backend.Perform(ctx, el, act)
	if err == nil {
		return &ActionResult{Method: "native", Action: act}, nil
	}
	if !isUnsupportedOrFailed(err) || !r.canFallback(el) {
		return nil, err
	}
	x, y := el.Bounds.Center()
	if perr := r.Physical.Scroll(x, y, amount); perr != nil {
		return nil, NewActionFailedError("native and physical scroll both failed: " + perr.Error())
	}
	return &ActionResult{
		Method: "physical",
		Action: act,
		Notes:  []string{"native scroll failed (" + err.Error() + "); fell back to physical scroll at element bounds center"},
	}, nil
}

// Focus focuses el: native Focus first, physical click as fallback.
func (r *ActionResolver) Focus(ctx context.Context, el *Element) (*ActionResult, error) {
	act := Action{Kind: ActionFocus}
	err := r.Backend.Perform(ctx, el, act)
	if err == nil {
		return &ActionResult{Method: "native", Action: act}, nil
	}
	if !isUnsupportedOrFailed(err) || !r.canFallback(el) {
		return nil, err
	}
	x, y := el.Bounds.Center()
	if perr := r.Physical.Click(x, y); perr != nil {
		return nil, NewActionFailedError("native and physical focus both failed: " + perr.Error())
	}
	return &ActionResult{
		Method: "physical",
		Action: act,
		Notes:  []string{"native focus failed (" + err.Error() + "); fell back to physical click at element bounds center"},
	}, nil
}

// Press sends a key/chord: native Press first, physical PressKey as
// fallback. When el is non-nil, the backend/physical input is asked to
// target it; a nil el means "send globally" (physical only).
func (r *ActionResolver) Press(ctx context.Context, el *Element, key string) (*ActionResult, error) {
	act := Action{Kind: ActionPress, Key: key}
	if el != nil {
		err := r.Backend.Perform(ctx, el, act)
		if err == nil {
			return &ActionResult{Method: "native", Action: act}, nil
		}
		if !isUnsupportedOrFailed(err) {
			return nil, err
		}
		if !r.AllowPhysical || r.Physical == nil {
			return nil, err
		}
		if perr := r.Physical.PressKey(key); perr != nil {
			return nil, NewActionFailedError("native and physical key press both failed: " + perr.Error())
		}
		return &ActionResult{
			Method: "physical",
			Action: act,
			Notes:  []string{"native press failed (" + err.Error() + "); fell back to physical key press"},
		}, nil
	}
	if !r.AllowPhysical || r.Physical == nil {
		return nil, NewPlatformNotSupportedError("no element target and physical input is not available for a global key press")
	}
	if perr := r.Physical.PressKey(key); perr != nil {
		return nil, NewActionFailedError("physical key press failed: " + perr.Error())
	}
	return &ActionResult{Method: "physical", Action: act}, nil
}
