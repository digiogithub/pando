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
			Path:      "main.go",
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
