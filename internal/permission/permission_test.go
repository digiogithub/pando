package permission

import (
	"context"
	"testing"
	"time"
)

func TestRequest_GlobalAutoApprove_DoesNotBlock(t *testing.T) {
	svc := NewPermissionService()
	svc.SetGlobalAutoApprove(true)

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID: "s1",
			ToolName:  "apply_patch",
			Action:    "write",
			Path:      "test.txt",
		})
	}()

	select {
	case approved := <-done:
		if !approved {
			t.Fatal("expected global auto-approve to return true")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("permission request blocked despite global auto-approve")
	}
}

func TestRequest_AutoApproveSession_DoesNotBlock(t *testing.T) {
	svc := NewPermissionService()
	svc.AutoApproveSession("session-auto")

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID: "session-auto",
			ToolName:  "edit_file",
			Action:    "write",
			Path:      "tmp/main.go",
		})
	}()

	select {
	case approved := <-done:
		if !approved {
			t.Fatal("expected auto-approved session to return true")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("permission request blocked despite session auto-approve")
	}
}

func TestRequest_RemoveAutoApproveSession_RestoresBlocking(t *testing.T) {
	svc := NewPermissionService()
	svc.AutoApproveSession("session-auto")
	svc.RemoveAutoApproveSession("session-auto")

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID: "session-auto",
			ToolName:  "edit_file",
			Action:    "write",
			Path:      "tmp/main.go",
		})
	}()

	select {
	case <-done:
		t.Fatal("expected request to block after removing session auto-approve")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRequest_RequireExplicitApprovalSession_BypassesSessionAutoApprove(t *testing.T) {
	svc := NewPermissionService()
	svc.AutoApproveSession("session-goal")
	svc.RequireExplicitApprovalSession("session-goal")

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID:               "session-goal",
			ToolName:                "bash",
			Action:                  "execute",
			Path:                    "tmp/main.go",
			RequireExplicitApproval: true,
		})
	}()

	select {
	case <-done:
		t.Fatal("expected explicit-approval request to block despite session auto-approve")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRequest_SessionHandler_DoesNotBlockAndUsesHandlerResult(t *testing.T) {
	svc := NewPermissionService()
	called := make(chan struct{}, 1)

	svc.RegisterSessionHandler("session-handler", func(req CreatePermissionRequest) bool {
		if req.SessionID != "session-handler" {
			t.Fatalf("unexpected sessionID: %s", req.SessionID)
		}
		if req.ToolName != "bash" {
			t.Fatalf("unexpected toolName: %s", req.ToolName)
		}
		if req.Description != "run command" {
			t.Fatalf("unexpected description: %s", req.Description)
		}
		called <- struct{}{}
		return true
	})

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID:   "session-handler",
			ToolName:    "bash",
			Description: "run command",
			Action:      "execute",
			Path:        "script.sh",
		})
	}()

	select {
	case <-called:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("session handler was not called")
	}

	select {
	case approved := <-done:
		if !approved {
			t.Fatal("expected session handler to approve the request")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("permission request blocked despite session handler")
	}
}

func TestRequest_UnregisterSessionHandler_RemovesCustomHandler(t *testing.T) {
	svc := NewPermissionService()
	svc.RegisterSessionHandler("session-handler", func(req CreatePermissionRequest) bool {
		return true
	})
	svc.UnregisterSessionHandler("session-handler")

	done := make(chan bool, 1)
	go func() {
		done <- svc.Request(CreatePermissionRequest{
			SessionID: "session-handler",
			ToolName:  "bash",
			Action:    "execute",
			Path:      "tmp/script.sh",
		})
	}()

	select {
	case <-done:
		t.Fatal("expected request without auto-approve or handler to block")
	case <-time.After(200 * time.Millisecond):
	}
}

// --------------------------------------------------- PANDO-US-0031: fail closed

// TestRequestWithContextFailsClosedWhenTheContextEnds is the PANDO-US-0031
// acceptance criterion for defect 3: an unanswered prompt must not block the
// calling tool's goroutine forever once the run that raised it is gone.
func TestRequestWithContextFailsClosedWhenTheContextEnds(t *testing.T) {
	svc := NewPermissionService()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool, 1)
	go func() {
		done <- svc.RequestWithContext(ctx, CreatePermissionRequest{
			SessionID: "s1", ToolName: "bash", Action: "execute", Path: "/repo",
		})
	}()

	// Wait until the request is actually published and pending, so the
	// cancellation below lands on a genuinely blocked wait.
	deadline := time.Now().Add(2 * time.Second)
	for len(svc.PendingRequests("s1")) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(svc.PendingRequests("s1")) == 0 {
		t.Fatal("the request never became pending")
	}

	cancel()
	select {
	case approved := <-done:
		if approved {
			t.Fatal("an abandoned permission request must fail closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestWithContext hung after its context ended")
	}
}

// TestRequestWithContextFailsClosedOnABlockedSessionHandler covers the AG-UI
// shape of the same defect: the blocking happens inside a registered session
// handler (the adapter suspends the run waiting for the browser), which must
// still be interruptible by the caller's context.
func TestRequestWithContextFailsClosedOnABlockedSessionHandler(t *testing.T) {
	svc := NewPermissionService()
	release := make(chan struct{})
	entered := make(chan struct{})
	svc.RegisterSessionHandler("s1", func(CreatePermissionRequest) bool {
		close(entered)
		<-release
		return true
	})
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- svc.RequestWithContext(ctx, CreatePermissionRequest{SessionID: "s1", ToolName: "bash"})
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the session handler was never called")
	}
	cancel()

	select {
	case approved := <-done:
		if approved {
			t.Fatal("a handler that never answered must fail closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestWithContext hung on a blocked session handler")
	}
}

// TestRequestDelegatesToRequestWithContext keeps the ~61 existing call sites
// honest: Request still blocks until answered, with no surprise deadline.
func TestRequestDelegatesToRequestWithContext(t *testing.T) {
	svc := NewPermissionService()
	svc.RegisterSessionHandler("s1", func(CreatePermissionRequest) bool { return true })
	if !svc.Request(CreatePermissionRequest{SessionID: "s1", ToolName: "bash"}) {
		t.Fatal("Request must still honour the session handler's verdict")
	}
}
