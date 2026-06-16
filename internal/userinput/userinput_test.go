package userinput

import (
	"context"
	"testing"
	"time"
)

func sampleQuestions() []Question {
	return []Question{
		{
			Header:   "Approach",
			Question: "Which approach do you prefer?",
			Options: []Option{
				{Label: "A", Description: "first"},
				{Label: "B", Description: "second"},
			},
		},
	}
}

func TestAsk_BlocksUntilRespond(t *testing.T) {
	svc := NewService()
	ctx := context.Background()
	events := svc.Subscribe(ctx)

	done := make(chan AskResponse, 1)
	go func() {
		done <- svc.Ask(CreateAskRequest{SessionID: "s1", Questions: sampleQuestions()})
	}()

	// Ask must block before a response is delivered.
	select {
	case <-done:
		t.Fatal("Ask returned before Respond was called")
	case <-time.After(100 * time.Millisecond):
	}

	// Grab the published request id from the event stream.
	var reqID string
	select {
	case ev := <-events:
		reqID = ev.Payload.ID
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no question request event published")
	}
	if reqID == "" {
		t.Fatal("expected non-empty request id")
	}

	svc.Respond(reqID, AskResponse{Answers: []Answer{{QuestionID: "q", Selected: []string{"A"}}}})

	select {
	case resp := <-done:
		if resp.Cancelled {
			t.Fatal("expected non-cancelled response")
		}
		if len(resp.Answers) != 1 || resp.Answers[0].Selected[0] != "A" {
			t.Fatalf("unexpected answers: %+v", resp.Answers)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Ask did not return after Respond")
	}
}

func TestCancel_UnblocksWithCancelled(t *testing.T) {
	svc := NewService()
	ctx := context.Background()
	events := svc.Subscribe(ctx)

	done := make(chan AskResponse, 1)
	go func() {
		done <- svc.Ask(CreateAskRequest{SessionID: "s1", Questions: sampleQuestions()})
	}()

	var reqID string
	select {
	case ev := <-events:
		reqID = ev.Payload.ID
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no question request event published")
	}

	svc.Cancel(reqID)

	select {
	case resp := <-done:
		if !resp.Cancelled {
			t.Fatal("expected cancelled response")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Ask did not return after Cancel")
	}
}

func TestPendingRequests_ReplayAndCleanup(t *testing.T) {
	svc := NewService()
	ctx := context.Background()
	events := svc.Subscribe(ctx)

	go svc.Ask(CreateAskRequest{SessionID: "s1", Questions: sampleQuestions()})

	var reqID string
	select {
	case ev := <-events:
		reqID = ev.Payload.ID
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no question request event published")
	}

	pending := svc.PendingRequests("s1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}

	svc.Respond(reqID, AskResponse{})
	// Allow the deferred cleanup in Ask to run.
	time.Sleep(50 * time.Millisecond)

	if pending := svc.PendingRequests("s1"); len(pending) != 0 {
		t.Fatalf("expected pending requests cleared, got %d", len(pending))
	}
}
