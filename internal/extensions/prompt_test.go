package extensions

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/pkg/extension"
)

func TestPromptRunnerIsAbsentUntilRegistered(t *testing.T) {
	SetPromptRunner(nil)
	if currentPromptRunner() != nil {
		t.Fatal("a host with no agent published a prompt runner")
	}

	SetPromptRunner(func(context.Context, extension.PromptRequest) (extension.PromptResult, error) {
		return extension.PromptResult{}, nil
	})
	t.Cleanup(func() { SetPromptRunner(nil) })
	if currentPromptRunner() == nil {
		t.Fatal("a registered runner was not published")
	}
}

func TestRunPromptRefusesAnEmptyPrompt(t *testing.T) {
	called := false
	SetPromptRunner(func(context.Context, extension.PromptRequest) (extension.PromptResult, error) {
		called = true
		return extension.PromptResult{}, nil
	})
	t.Cleanup(func() { SetPromptRunner(nil) })

	if _, err := currentPromptRunner().RunPrompt(context.Background(), extension.PromptRequest{}); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
	if called {
		t.Error("an empty prompt reached the agent")
	}
}

func TestRunPromptContainsProgressPanic(t *testing.T) {
	SetPromptRunner(func(_ context.Context, req extension.PromptRequest) (extension.PromptResult, error) {
		// The host calls the callback the same way the agent loop would.
		req.OnProgress(extension.PromptProgress{Delta: "hi"})
		return extension.PromptResult{Text: "done"}, nil
	})
	t.Cleanup(func() { SetPromptRunner(nil) })

	res, err := currentPromptRunner().RunPrompt(context.Background(), extension.PromptRequest{
		Prompt:     "say hello",
		OnProgress: func(extension.PromptProgress) { panic("boom") },
	})
	if err != nil {
		t.Fatalf("a panicking callback failed the run: %v", err)
	}
	if res.Text != "done" {
		t.Errorf("result = %+v", res)
	}
}

func TestRunPromptPassesTheRequestThrough(t *testing.T) {
	var got extension.PromptRequest
	SetPromptRunner(func(_ context.Context, req extension.PromptRequest) (extension.PromptResult, error) {
		got = req
		return extension.PromptResult{SessionID: "s1", Text: "ok", FinishReason: "end_turn"}, nil
	})
	t.Cleanup(func() { SetPromptRunner(nil) })

	res, err := currentPromptRunner().RunPrompt(context.Background(), extension.PromptRequest{
		Prompt:      "run the pipeline",
		Title:       "job-1",
		AutoApprove: true,
	})
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if got.Prompt != "run the pipeline" || got.Title != "job-1" || !got.AutoApprove {
		t.Errorf("request = %+v", got)
	}
	if res.SessionID != "s1" || res.Text != "ok" || res.FinishReason != "end_turn" {
		t.Errorf("result = %+v", res)
	}
}
