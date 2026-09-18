package permission

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
)

// answerAll subscribes to svc and answers every published request with
// answer, returning the published requests as they arrive.
func answerAll(t *testing.T, svc Service, answer func(PermissionRequest)) <-chan PermissionRequest {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := svc.Subscribe(ctx)
	out := make(chan PermissionRequest, 16)
	go func() {
		for ev := range events {
			if ev.Type == pubsub.CreatedEvent {
				out <- ev.Payload
				answer(ev.Payload)
			}
		}
	}()
	return out
}

func escalation(key string) CreatePermissionRequest {
	return CreatePermissionRequest{
		SessionID:               "s1",
		ToolName:                "bash",
		Action:                  ActionExecuteUnsandboxed,
		Path:                    "/ws",
		RequireExplicitApproval: true,
		NeverAutoApprove:        true,
		Justification:           "why",
		GrantKey:                key,
	}
}

func TestNeverAutoApprove_IgnoresAutoApproveModes(t *testing.T) {
	svc := NewPermissionService()
	svc.SetGlobalAutoApprove(true)
	svc.AutoApproveSession("s1")
	// An unscoped session grant for the same tool/action/path.
	svc.GrantPersistant(PermissionRequest{SessionID: "s1", ToolName: "bash", Action: ActionExecuteUnsandboxed, Path: "/"})
	published := answerAll(t, svc, svc.Deny)

	if svc.Request(escalation("")) {
		t.Fatal("NeverAutoApprove request was auto-approved")
	}
	select {
	case p := <-published:
		if !p.NeverAutoApprove || !p.RequireExplicitApproval || p.Justification != "why" {
			t.Fatalf("published request lost its fields: %+v", p)
		}
	default:
		t.Fatal("request was not published for an explicit answer")
	}
}

func TestNeverAutoApprove_DeniedWhenNobodyListens(t *testing.T) {
	svc := NewPermissionService()
	svc.SetGlobalAutoApprove(true)

	done := make(chan bool, 1)
	go func() { done <- svc.Request(escalation("")) }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("approved without anybody to answer")
		}
	case <-time.After(time.Second):
		t.Fatal("request blocked although nobody can answer it")
	}
}

func TestGrantKey_ScopesSessionGrants(t *testing.T) {
	svc := NewPermissionService()
	published := answerAll(t, svc, svc.GrantPersistant)

	if !svc.Request(escalation("prefix:npm install")) {
		t.Fatal("first request not granted")
	}
	<-published
	if !svc.Request(escalation("prefix:npm install")) {
		t.Fatal("same key not granted")
	}
	select {
	case p := <-published:
		t.Fatalf("same key prompted again: %+v", p)
	default:
	}
	if !svc.Request(escalation("prefix:npm test")) {
		t.Fatal("other key not granted by the answering UI")
	}
	select {
	case <-published:
	default:
		t.Fatal("a different key reused the grant")
	}
}

func TestGrantKey_EmptyKeyKeepsHistoricalScope(t *testing.T) {
	svc := NewPermissionService()
	published := answerAll(t, svc, svc.GrantPersistant)
	req := CreatePermissionRequest{SessionID: "s1", ToolName: "bash", Action: "execute", Path: "/ws/x"}

	if !svc.Request(req) {
		t.Fatal("first request not granted")
	}
	<-published
	if !svc.Request(req) {
		t.Fatal("second request not granted by the session grant")
	}
	select {
	case <-published:
		t.Fatal("unkeyed session grant no longer reused")
	default:
	}
}
