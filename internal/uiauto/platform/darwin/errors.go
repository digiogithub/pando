package darwin

import (
	"fmt"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// AXError mirrors the AXError codes documented in
// <ApplicationServices/HIServices/AXError.h>. Only the codes the plan calls
// out explicitly get a name; anything else maps generically.
type AXError int32

const (
	AXErrorSuccess                         AXError = 0
	AXErrorFailure                         AXError = -25200
	AXErrorIllegalArgument                 AXError = -25201
	AXErrorInvalidUIElement                AXError = -25202
	AXErrorInvalidUIElementObserver        AXError = -25203
	AXErrorCannotComplete                  AXError = -25204
	AXErrorAttributeUnsupported            AXError = -25205
	AXErrorActionUnsupported               AXError = -25206
	AXErrorNotificationUnsupported         AXError = -25207
	AXErrorNotImplemented                  AXError = -25208
	AXErrorNotificationAlreadyRegistered   AXError = -25209
	AXErrorNotificationNotRegistered       AXError = -25210
	AXErrorAPIDisabled                     AXError = -25211
	AXErrorNoValue                         AXError = -25212
	AXErrorParameterizedAttributeUnsupport AXError = -25213
	AXErrorNotEnoughPrecision              AXError = -25214
)

// mapAXError converts a nonzero AXError returned by an AX call (context
// names the attribute/action/call site, for the DesktopError message) into
// the corresponding *core.DesktopError. Callers must check for
// AXErrorSuccess (0) themselves; mapAXError(0, ...) still returns nil as a
// convenience.
func mapAXError(code AXError, context string) *core.DesktopError {
	switch code {
	case AXErrorSuccess:
		return nil
	case AXErrorInvalidUIElement, AXErrorInvalidUIElementObserver:
		return core.NewStaleRefError(fmt.Sprintf("%s: the AXUIElement is no longer valid (kAXErrorInvalidUIElement)", context))
	case AXErrorAPIDisabled:
		return core.NewPermDeniedError(fmt.Sprintf("%s: the Accessibility API is disabled for this process (kAXErrorAPIDisabled)", context))
	case AXErrorAttributeUnsupported, AXErrorParameterizedAttributeUnsupport:
		return core.NewActionFailedError(fmt.Sprintf("%s: attribute not supported by this element (kAXErrorAttributeUnsupported)", context))
	case AXErrorActionUnsupported:
		return core.NewActionFailedError(fmt.Sprintf("%s: action not supported by this element (kAXErrorActionUnsupported)", context))
	case AXErrorNoValue:
		return core.NewActionFailedError(fmt.Sprintf("%s: attribute has no value (kAXErrorNoValue)", context))
	case AXErrorCannotComplete:
		return core.NewActionFailedError(fmt.Sprintf("%s: the request could not be completed, the application may not be responding (kAXErrorCannotComplete)", context))
	case AXErrorNotImplemented:
		return core.NewPlatformNotSupportedError(fmt.Sprintf("%s: not implemented by this element/process (kAXErrorNotImplemented)", context))
	case AXErrorIllegalArgument:
		return core.NewInvalidArgsError(fmt.Sprintf("%s: illegal argument (kAXErrorIllegalArgument)", context))
	default:
		return core.NewActionFailedError(fmt.Sprintf("%s: AXError %d", context, int32(code)))
	}
}
