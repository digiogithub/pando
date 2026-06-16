package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/userinput"
)

const askCallInput = `{"questions":[{"question":"Which approach?","header":"Approach","multiSelect":false,"options":[{"label":"A","description":"first"},{"label":"B","description":"second"}]}]}`

func TestAskUserQuestion_ACPMode_ReturnsText(t *testing.T) {
	tool := NewAskUserQuestionTool(userinput.NewService())

	ctx := context.WithValue(context.Background(), ACPClientConnContextKey, struct{}{})
	resp, err := tool.Run(ctx, ToolCall{Input: askCallInput})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Which approach?") {
		t.Fatalf("expected question text in ACP response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "A — first") {
		t.Fatalf("expected formatted option in ACP response, got: %s", resp.Content)
	}
}

func TestAskUserQuestion_UIMode_BlocksAndReturnsSelection(t *testing.T) {
	svc := userinput.NewService()
	tool := NewAskUserQuestionTool(svc)

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s1")
	events := svc.Subscribe(context.Background())

	type result struct {
		resp ToolResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		r, e := tool.Run(ctx, ToolCall{Input: askCallInput})
		done <- result{r, e}
	}()

	// Capture the published request and respond.
	select {
	case ev := <-events:
		svc.Respond(ev.Payload.ID, userinput.AskResponse{
			Answers: []userinput.Answer{{QuestionID: "q1", Selected: []string{"A"}}},
		})
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no question request published")
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if r.resp.IsError {
			t.Fatalf("unexpected error response: %s", r.resp.Content)
		}
		if !strings.Contains(r.resp.Content, "A") {
			t.Fatalf("expected selection in response, got: %s", r.resp.Content)
		}
		if !strings.Contains(r.resp.Metadata, "\"selected\"") {
			t.Fatalf("expected structured metadata, got: %s", r.resp.Metadata)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("tool did not return after Respond")
	}
}

func TestAskUserQuestion_Cancelled(t *testing.T) {
	svc := userinput.NewService()
	tool := NewAskUserQuestionTool(svc)

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s1")
	events := svc.Subscribe(context.Background())

	done := make(chan ToolResponse, 1)
	go func() {
		r, _ := tool.Run(ctx, ToolCall{Input: askCallInput})
		done <- r
	}()

	select {
	case ev := <-events:
		svc.Cancel(ev.Payload.ID)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no question request published")
	}

	select {
	case r := <-done:
		if !r.IsError {
			t.Fatalf("expected error response on cancel, got: %s", r.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("tool did not return after Cancel")
	}
}

func TestAskUserQuestion_Validation(t *testing.T) {
	tool := NewAskUserQuestionTool(userinput.NewService())
	ctx := context.Background()

	// Only one option -> invalid.
	resp, _ := tool.Run(ctx, ToolCall{Input: `{"questions":[{"question":"Q","header":"H","options":[{"label":"A","description":"x"}]}]}`})
	if !resp.IsError {
		t.Fatalf("expected validation error for too few options, got: %s", resp.Content)
	}
}
