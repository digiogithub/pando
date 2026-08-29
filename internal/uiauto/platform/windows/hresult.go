package windows

import (
	"fmt"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Well-known HRESULT values this backend classifies specially. Values are
// the standard Win32/COM/UIA constants (winerror.h / uiautomationcoreapi.h),
// kept here (rather than only in the windows-tagged COM call sites) so the
// classification table itself is unit-testable on any platform.
const (
	hrOK                         uint32 = 0x00000000
	hrEAccessDenied              uint32 = 0x80070005
	hrERPCUnavailable            uint32 = 0x800706BA // RPC_S_SERVER_UNAVAILABLE (target process exited)
	hrEInvalidArg                uint32 = 0x80070057
	hrENotImpl                   uint32 = 0x80004001
	hrUIAElementNotAvailable     uint32 = 0x80040201 // UIA_E_ELEMENTNOTAVAILABLE
	hrUIAElementNotEnabled       uint32 = 0x80040200 // UIA_E_ELEMENTNOTENABLED
	hrUIANoClickablePoint        uint32 = 0x80040202 // UIA_E_NOCLICKABLEPOINT
	hrUIAProxyAssemblyNotLoaded  uint32 = 0x80040203
	hrUIAPatternNotSupported     uint32 = 0x80040204
	hrUIAInvalidOperation        uint32 = 0x80131509
	hrCOAlreadyInitialized       uint32 = 0x80010106
	hrCONotInitialized           uint32 = 0x800401F0
	hrRegDBClassNotReg           uint32 = 0x80040154 // CLASS_E_CLASSNOTAVAILABLE
	hrUIAAsyncContentLoadedNoErr uint32 = 0x00131515
)

// mapHRESULT classifies a Windows HRESULT returned by a UIA/COM call into a
// core.DesktopError, tagging op (a short description such as "GetRootElement"
// or "InvokePattern.Invoke") into the message for diagnosability. This is a
// best-effort classification over the handful of HRESULTs UIA client code
// most commonly returns; any HRESULT not specifically recognized maps to
// ACTION_FAILED (for a call attempted against a resolved element) which lets
// core.ActionResolver's native-first/physical-fallback policy take over, or
// the caller-provided fallback code for calls that are not an element
// action (e.g. backend/COM setup itself).
func mapHRESULT(op string, hr uint32, fallback core.ErrorCode) error {
	if hr == hrOK {
		return nil
	}
	msg := fmt.Sprintf("%s failed: HRESULT 0x%08X", op, hr)
	switch hr {
	case hrEAccessDenied, hrRegDBClassNotReg:
		return core.NewPermDeniedError(msg)
	case hrERPCUnavailable:
		return core.NewElementNotFoundError(msg + " (the owning process may have exited)")
	case hrUIAElementNotAvailable:
		return core.NewStaleRefError(msg)
	case hrUIAElementNotEnabled:
		return core.NewActionFailedError(msg + " (element is disabled)")
	case hrUIANoClickablePoint:
		return core.NewActionFailedError(msg + " (no clickable point on element)")
	case hrUIAPatternNotSupported, hrENotImpl:
		return core.NewActionFailedError(msg + " (pattern/operation not supported by this element)")
	case hrEInvalidArg:
		return core.NewInvalidArgsError(msg)
	}
	return newDesktopErrorForFallback(fallback, msg)
}

// newDesktopErrorForFallback builds a *core.DesktopError of the requested
// code with msg, covering every ErrorCode a caller of mapHRESULT might pass
// as fallback.
func newDesktopErrorForFallback(code core.ErrorCode, msg string) error {
	switch code {
	case core.ErrPermDenied:
		return core.NewPermDeniedError(msg)
	case core.ErrElementNotFound:
		return core.NewElementNotFoundError(msg)
	case core.ErrAppNotFound:
		return core.NewAppNotFoundError(msg)
	case core.ErrStaleRef:
		return core.NewStaleRefError(msg)
	case core.ErrSnapshotNotFound:
		return core.NewSnapshotNotFoundError(msg)
	case core.ErrPolicyDenied:
		return core.NewPolicyDeniedError(msg)
	case core.ErrPlatformNotSupported:
		return core.NewPlatformNotSupportedError(msg)
	case core.ErrTimeout:
		return core.NewTimeoutError(msg)
	case core.ErrInvalidArgs:
		return core.NewInvalidArgsError(msg)
	default:
		return core.NewActionFailedError(msg)
	}
}
