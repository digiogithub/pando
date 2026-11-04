package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
)

const sseTestChunk = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}`

// sseTestFinalChunk closes the stream the way real servers do: a stop reason,
// then the usage chunk requested through stream_options.include_usage.
const sseTestFinalChunk = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`

// decodeChatChunks runs body through the decoder the SDK picks for contentType
// (after the same MIME normalisation the clients apply) and counts the chunks.
func decodeChatChunks(t *testing.T, contentType, body string) (int, error) {
	t.Helper()
	res := &http.Response{
		Header: http.Header{"Content-Type": {contentType}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	resp, err := sseNormalizeMimeMiddleware(&http.Request{}, func(*http.Request) (*http.Response, error) { return res, nil })
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	stream := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(resp), nil)
	n := 0
	for stream.Next() {
		n++
	}
	return n, stream.Err()
}

// TestSSEDecoderSkipsKeepaliveComments: a comment-only block (": ping") is a
// keepalive, not an event. The blank line that closes it must not dispatch an
// empty payload, which used to fail every stream with "unexpected end of JSON
// input" as soon as a slow model let the server send one.
func TestSSEDecoderSkipsKeepaliveComments(t *testing.T) {
	cases := map[string]string{
		"no comment":              "data: " + sseTestChunk + "\n\ndata: [DONE]\n\n",
		"leading ping":            ": ping\n\ndata: " + sseTestChunk + "\n\ndata: [DONE]\n\n",
		"pings between and after": ": ping\n\n: ping\n\ndata: " + sseTestChunk + "\n\n:\n\ndata: [DONE]\n\n: ping\n\n",
		"comment inside an event": "data: " + sseTestChunk + "\n: keepalive\n\ndata: [DONE]\n\n",
		"blank lines only":        "\n\n\ndata: " + sseTestChunk + "\n\ndata: [DONE]\n\n",
		"named event, no data":    "event: ping\n\ndata: " + sseTestChunk + "\n\ndata: [DONE]\n\n",
	}
	for _, ct := range []string{"text/event-stream", "text/event-stream; charset=utf-8"} {
		for name, body := range cases {
			t.Run(ct+"/"+name, func(t *testing.T) {
				n, err := decodeChatChunks(t, ct, body)
				if err != nil {
					t.Fatalf("stream error: %v", err)
				}
				if n != 1 {
					t.Fatalf("chunks = %d, want 1", n)
				}
			})
		}
	}
}

// TestSSEDecoderStillReportsMalformedData: skipping empty blocks must not hide
// a real payload error.
func TestSSEDecoderStillReportsMalformedData(t *testing.T) {
	_, err := decodeChatChunks(t, "text/event-stream", ": ping\n\ndata: {not json\n\n")
	if err == nil {
		t.Fatal("expected a decode error for a malformed payload")
	}
}

// TestOpenAICompatibleStreamSurvivesKeepaliveComments drives a real client
// built by newOpenAIClient (the path of openai, openai-compatible, groq,
// openrouter, xai, ollama, llama.cpp, local and azure) against a server that
// interleaves ": ping" comments and adds a charset to its Content-Type, which
// is what bypassed Pando's decoder before.
func TestOpenAICompatibleStreamSurvivesKeepaliveComments(t *testing.T) {
	setProviderConfigForTests(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			fmt.Fprint(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		write(": ping\n\n")
		write("data: " + sseTestChunk + "\n\n")
		write(": ping\n\n")
		write("data: " + sseTestFinalChunk + "\n\n")
		write("data: [DONE]\n\n")
	}))
	defer srv.Close()

	model := models.Model{ID: "m", APIModel: "m", Provider: models.ProviderOpenAICompatible, DefaultMaxTokens: 256}
	client := newOpenAIClient(providerClientOptions{
		apiKey:        "k",
		model:         model,
		maxTokens:     256,
		maxRetries:    1,
		openaiOptions: []OpenAIOption{WithOpenAIBaseURL(srv.URL)},
	}).(*openaiClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var content strings.Builder
	var completed bool
	for ev := range client.stream(ctx, userMsgs(), nil) {
		switch ev.Type {
		case EventError:
			t.Fatalf("stream failed: %v", ev.Error)
		case EventContentDelta:
			content.WriteString(ev.Content)
		case EventComplete:
			completed = true
		}
	}
	if !completed || content.String() != "hi" {
		t.Fatalf("completed=%v content=%q, want a completed stream with %q", completed, content.String(), "hi")
	}
}
