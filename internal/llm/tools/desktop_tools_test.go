package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto"
)

const desktopTestSessionID = "desktop-test-session"

// withDesktopTestConfig installs a config whose InternalTools force the
// uiauto Manager singleton onto the "null" backend (deterministic,
// zero-OS-call), runs fn, then restores whatever config was there before and
// resets the shared Manager so later tests do not observe this one's state.
func withDesktopTestConfig(t *testing.T, mutate func(*config.InternalToolsConfig), fn func()) {
	t.Helper()
	previous := config.Get()
	it := config.InternalToolsConfig{
		DesktopEnabled:            true,
		DesktopBackend:            "null",
		DesktopAllowPhysicalInput: true,
		DesktopMaxNodes:           50,
		DesktopDefaultDepth:       3,
		DesktopActionTimeout:      1,
		DesktopSnapshotTTL:        60,
		DesktopScreenshotScale:    1.0,
	}
	if mutate != nil {
		mutate(&it)
	}
	config.SetForTests(&config.Config{InternalTools: it})
	uiauto.ResetShared()
	t.Cleanup(func() {
		config.SetForTests(previous)
		uiauto.ResetShared()
	})
	fn()
}

func desktopTestCtx() context.Context {
	return context.WithValue(context.Background(), SessionIDContextKey, desktopTestSessionID)
}

// newTestPermissionService returns a real permission.Service configured to
// either always grant (via global auto-approve) or always deny (via a
// per-session handler) requests for desktopTestSessionID.
func newTestPermissionService(grant bool) permission.Service {
	svc := permission.NewPermissionService()
	if grant {
		svc.SetGlobalAutoApprove(true)
		return svc
	}
	svc.RegisterSessionHandler(desktopTestSessionID, func(req permission.CreatePermissionRequest) bool {
		return false
	})
	return svc
}

func assertErrorCode(t *testing.T, resp ToolResponse, code string) {
	t.Helper()
	if !resp.IsError {
		t.Fatalf("expected an error response, got: %+v", resp)
	}
	if !strings.Contains(resp.Content, code) {
		t.Fatalf("expected response to contain %q, got: %s", code, resp.Content)
	}
}

// ---- desktop_apps ----

func TestDesktopAppsToolPlatformNotSupported(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopAppsTool()
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: "{}"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PLATFORM_NOT_SUPPORTED")
	})
}

func TestDesktopAppsToolBadInput(t *testing.T) {
	tool := NewDesktopAppsTool()
	resp, err := tool.Run(context.Background(), ToolCall{Input: "not json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "failed to parse parameters") {
		t.Fatalf("expected a parse-error response, got: %+v", resp)
	}
}

// ---- desktop_observe / desktop_find / desktop_read (read-only) ----

func TestDesktopObserveToolPlatformNotSupported(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopObserveTool()
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"app_id":"app1"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PLATFORM_NOT_SUPPORTED")
	})
}

func TestDesktopFindToolRequiresSelector(t *testing.T) {
	tool := NewDesktopFindTool()
	resp, err := tool.Run(context.Background(), ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertErrorCode(t, resp, "INVALID_ARGS")
}

func TestDesktopFindToolPlatformNotSupported(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopFindTool()
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"selector":"button[name=\"Save\"]"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PLATFORM_NOT_SUPPORTED")
	})
}

func TestDesktopReadToolRequiresRef(t *testing.T) {
	tool := NewDesktopReadTool()
	resp, err := tool.Run(context.Background(), ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertErrorCode(t, resp, "INVALID_ARGS")
}

func TestDesktopReadToolStaleRef(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopReadTool()
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@snonexistent:e1"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "SNAPSHOT_NOT_FOUND")
	})
}

// ---- desktop_wait (read-only) ----

func TestDesktopWaitToolInvalidCondition(t *testing.T) {
	tool := NewDesktopWaitTool()
	resp, err := tool.Run(context.Background(), ToolCall{Input: `{"selector":"button","condition":"bogus"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertErrorCode(t, resp, "INVALID_ARGS")
}

// ---- mutating tools: permission gate ----

func TestDesktopClickToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopClickTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@s1:e1"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopClickToolPermissionGrantedThenPlatformNotSupported(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopClickTool(newTestPermissionService(true))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@s1:e1"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Permission was granted, so the failure comes from the resolved
		// (nonexistent) ref/backend, never PERM_DENIED.
		if strings.Contains(resp.Content, "PERM_DENIED") {
			t.Fatalf("permission should have been granted, got: %s", resp.Content)
		}
		if !resp.IsError {
			t.Fatalf("expected an error response for an unresolved ref, got: %+v", resp)
		}
	})
}

// ---- desktop_click_at (vision fallback) ----

func TestDesktopClickAtToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopClickAtTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"x":10,"y":10}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopClickAtToolPermissionGrantedThenFails(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopClickAtTool(newTestPermissionService(true))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"x":10,"y":10}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Permission was granted, so the failure (this dev box has no
		// usable physical input session) never comes from PERM_DENIED.
		if strings.Contains(resp.Content, "PERM_DENIED") {
			t.Fatalf("permission should have been granted, got: %s", resp.Content)
		}
		if !resp.IsError {
			t.Fatalf("expected an error response (no physical input available here), got: %+v", resp)
		}
	})
}

func TestDesktopClickAtToolDisallowedByPolicy(t *testing.T) {
	withDesktopTestConfig(t, func(it *config.InternalToolsConfig) {
		it.DesktopAllowPhysicalInput = false
	}, func() {
		tool := NewDesktopClickAtTool(newTestPermissionService(true))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"x":10,"y":10}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "POLICY_DENIED")
	})
}

func TestDesktopTypeToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopTypeTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@s1:e1","text":"hi"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopKeyToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopKeyTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"key":"Enter"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopScrollToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopScrollTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@s1:e1","amount":10}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopFocusToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopFocusTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"ref":"@s1:e1"}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

// desktop_screenshot is read-only but SHOULD prompt (captures the whole
// screen), and must report PLATFORM_NOT_SUPPORTED (no screen backend in
// Phase 1) once permission is granted.
func TestDesktopScreenshotToolPermissionDenied(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopScreenshotTool(newTestPermissionService(false))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertErrorCode(t, resp, "PERM_DENIED")
	})
}

func TestDesktopScreenshotToolPlatformNotSupportedWhenGranted(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopScreenshotTool(newTestPermissionService(true))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.IsError || resp.Type != ToolResponseTypeText {
			t.Fatalf("expected a structured text error response, got: %+v", resp)
		}
		if !strings.Contains(resp.Content, "PLATFORM_NOT_SUPPORTED") {
			t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got: %s", resp.Content)
		}
	})
}

// TestDesktopScreenshotToolGridParamDecodes verifies the grid:true param
// decodes and reaches the (still PLATFORM_NOT_SUPPORTED, null-backend)
// Manager.Screenshot call without a decode error -- the grid overlay
// itself is unit-tested directly in internal/uiauto/vision.
func TestDesktopScreenshotToolGridParamDecodes(t *testing.T) {
	withDesktopTestConfig(t, nil, func() {
		tool := NewDesktopScreenshotTool(newTestPermissionService(true))
		resp, err := tool.Run(desktopTestCtx(), ToolCall{Input: `{"grid":true}`})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.IsError {
			t.Fatalf("expected a PLATFORM_NOT_SUPPORTED error response, got: %+v", resp)
		}
		if !strings.Contains(resp.Content, "PLATFORM_NOT_SUPPORTED") {
			t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got: %s", resp.Content)
		}
	})
}
