package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const okChatBody = `{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
const emptyChatBody = `{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[]}`

func emptyChoicesModel() models.Model {
	return models.Model{ID: "haiku", APIModel: "haiku", Provider: models.ProviderCopilot, DefaultMaxTokens: 1024}
}

func userMsgs() []message.Message {
	return []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}}}
}

// emptyThenOKServer answers `empty` times with no choices, then with a valid body.
func emptyThenOKServer(empty int32, hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		if n <= empty {
			fmt.Fprint(w, emptyChatBody)
			return
		}
		fmt.Fprint(w, okChatBody)
	}))
}

func newEmptyChoicesCopilot(t *testing.T, url string) *copilotClient {
	setProviderConfigForTests(t)
	c := newTestCopilotClient(t, url, emptyChoicesModel(), "medium")
	c.providerOptions.maxRetries = 1
	return c
}

func newEmptyChoicesOpenAI(url string) *openaiClient {
	return &openaiClient{
		providerOptions: providerClientOptions{model: emptyChoicesModel(), maxTokens: 1024, maxRetries: 1},
		client:          openai.NewClient(option.WithBaseURL(url), option.WithAPIKey("k")),
	}
}

func TestCopilotSendEmptyChoicesReturnsErrorWithoutPanic(t *testing.T) {
	var hits int32
	srv := emptyThenOKServer(100, &hits)
	defer srv.Close()
	c := newEmptyChoicesCopilot(t, srv.URL)
	_, err := c.send(context.Background(), userMsgs(), nil)
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected empty-choices error, got %v", err)
	}
}

func TestCopilotSendEmptyChoicesRetriesThenSucceeds(t *testing.T) {
	var hits int32
	srv := emptyThenOKServer(1, &hits)
	defer srv.Close()
	c := newEmptyChoicesCopilot(t, srv.URL)
	resp, err := c.send(context.Background(), userMsgs(), nil)
	if err != nil {
		t.Fatalf("send() error = %v", err)
	}
	if resp.Content != "hello" || atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("content=%q hits=%d", resp.Content, hits)
	}
}

func TestOpenAISendEmptyChoicesReturnsErrorWithoutPanic(t *testing.T) {
	setProviderConfigForTests(t)
	var hits int32
	srv := emptyThenOKServer(100, &hits)
	defer srv.Close()
	_, err := newEmptyChoicesOpenAI(srv.URL).send(context.Background(), userMsgs(), nil)
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected empty-choices error, got %v", err)
	}
}

func TestOpenAISendEmptyChoicesRetriesThenSucceeds(t *testing.T) {
	setProviderConfigForTests(t)
	var hits int32
	srv := emptyThenOKServer(1, &hits)
	defer srv.Close()
	resp, err := newEmptyChoicesOpenAI(srv.URL).send(context.Background(), userMsgs(), nil)
	if err != nil || resp.Content != "hello" {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
}

func TestCopilotStreamWithoutChoicesCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c := newEmptyChoicesCopilot(t, srv.URL)
	var got *ProviderResponse
	for ev := range c.stream(context.Background(), userMsgs(), nil) {
		if ev.Type == EventError {
			t.Fatalf("stream error: %v", ev.Error)
		}
		if ev.Type == EventComplete {
			got = ev.Response
		}
	}
	if got == nil || got.FinishReason != message.FinishReasonUnknown {
		t.Fatalf("expected EventComplete with unknown finish reason, got %+v", got)
	}
}
