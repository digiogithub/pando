package acp

import (
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func newModelSyncSession() *ACPServerSession {
	return NewACPServerSession(acpsdk.SessionId("acp-1"), "/tmp", nil, "pando-1")
}

func TestReconcileACPSessionModelAdoptsRuntimeSwitch(t *testing.T) {
	svc := &mockAgentService{
		sessionModelOverrides: map[string]string{"pando-1": "switched-model"},
	}
	session := newModelSyncSession()
	session.SetModel("picked-model")

	if !reconcileACPSessionModel(svc, session) {
		t.Fatalf("expected the runtime switch to be adopted")
	}
	if got := session.Model(); got != "switched-model" {
		t.Fatalf("session model = %q, want switched-model", got)
	}

	// Idempotent: a second pass has nothing left to adopt.
	if reconcileACPSessionModel(svc, session) {
		t.Fatalf("expected no change on the second reconciliation")
	}
}

func TestReconcileACPSessionModelIgnoresGlobalModel(t *testing.T) {
	// No override: the session keeps what the client picked even though the
	// agent's globally configured model is a different one.
	svc := &mockAgentService{currentModel: "global-model"}
	session := newModelSyncSession()
	session.SetModel("picked-model")

	if reconcileACPSessionModel(svc, session) {
		t.Fatalf("expected no reconciliation without a session override")
	}
	if got := session.Model(); got != "picked-model" {
		t.Fatalf("session model = %q, want picked-model", got)
	}
}

func TestReconcileACPSessionModelAdoptsOntoEmptySelection(t *testing.T) {
	svc := &mockAgentService{
		sessionModelOverrides: map[string]string{"pando-1": "switched-model"},
	}
	session := newModelSyncSession()

	if !reconcileACPSessionModel(svc, session) {
		t.Fatalf("expected the switch to be adopted when no model was picked")
	}
	if got := session.Model(); got != "switched-model" {
		t.Fatalf("session model = %q, want switched-model", got)
	}
}

func TestReconcileACPSessionModelHandlesNilInputs(t *testing.T) {
	if reconcileACPSessionModel(nil, newModelSyncSession()) {
		t.Fatalf("expected false for a nil service")
	}
	if reconcileACPSessionModel(&mockAgentService{}, nil) {
		t.Fatalf("expected false for a nil session")
	}
}
