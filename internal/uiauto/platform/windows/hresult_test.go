package windows

import (
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestMapHRESULT(t *testing.T) {
	cases := []struct {
		name     string
		hr       uint32
		fallback core.ErrorCode
		want     core.ErrorCode
	}{
		{"ok", hrOK, core.ErrActionFailed, ""},
		{"access denied", hrEAccessDenied, core.ErrActionFailed, core.ErrPermDenied},
		{"class not registered", hrRegDBClassNotReg, core.ErrActionFailed, core.ErrPermDenied},
		{"rpc unavailable", hrERPCUnavailable, core.ErrActionFailed, core.ErrElementNotFound},
		{"element not available", hrUIAElementNotAvailable, core.ErrActionFailed, core.ErrStaleRef},
		{"element not enabled", hrUIAElementNotEnabled, core.ErrActionFailed, core.ErrActionFailed},
		{"no clickable point", hrUIANoClickablePoint, core.ErrActionFailed, core.ErrActionFailed},
		{"pattern not supported", hrUIAPatternNotSupported, core.ErrActionFailed, core.ErrActionFailed},
		{"not implemented", hrENotImpl, core.ErrActionFailed, core.ErrActionFailed},
		{"invalid arg", hrEInvalidArg, core.ErrActionFailed, core.ErrInvalidArgs},
		{"unknown falls back to platform-not-supported", 0xDEADBEEF, core.ErrPlatformNotSupported, core.ErrPlatformNotSupported},
		{"unknown falls back to action-failed", 0xDEADBEEF, core.ErrActionFailed, core.ErrActionFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mapHRESULT("Test.Op", c.hr, c.fallback)
			if c.want == "" {
				if err != nil {
					t.Fatalf("mapHRESULT(ok) = %v, want nil", err)
				}
				return
			}
			de, ok := core.AsDesktopError(err)
			if !ok {
				t.Fatalf("mapHRESULT(%s) did not return a DesktopError: %v", c.name, err)
			}
			if de.Code != c.want {
				t.Fatalf("mapHRESULT(%s) code = %s, want %s", c.name, de.Code, c.want)
			}
			if de.Message == "" {
				t.Fatalf("mapHRESULT(%s) has empty message", c.name)
			}
		})
	}
}
