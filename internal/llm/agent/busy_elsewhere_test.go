package agent

import (
	"context"
	"errors"
	"testing"
)

func TestRunRefusesSessionBusyElsewhere(t *testing.T) {
	SetExternalRunCheck(func(sessionID string) bool { return sessionID == "ext" })
	t.Cleanup(func() { SetExternalRunCheck(nil) })
	a := newResumeTestAgent(t)

	if _, err := a.Run(context.Background(), "ext", "hello"); !errors.Is(err, ErrSessionBusyElsewhere) {
		t.Fatalf("Run err = %v, want ErrSessionBusyElsewhere", err)
	}
	if _, err := a.ResumeRun(context.Background(), "ext", "resume"); !errors.Is(err, ErrSessionBusyElsewhere) {
		t.Fatalf("ResumeRun err = %v, want ErrSessionBusyElsewhere", err)
	}
	if a.IsSessionBusy("ext") {
		t.Fatal("a refused run must not register as active")
	}

	SetExternalRunCheck(nil)
	if sessionBusyElsewhere("ext") {
		t.Fatal("removing the check must release the session")
	}
}
