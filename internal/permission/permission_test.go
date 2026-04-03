package permission

import (
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
