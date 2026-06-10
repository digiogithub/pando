package provider

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	"github.com/openai/openai-go"
)

// TestConvertMessagesReInjectsThinkTags verifies that for Ollama (parseThinkTags=true),
// the thinking content is re-injected as <think>...</think> in the assistant message
// so multi-turn conversations preserve the model's reasoning context.
func TestConvertMessagesReInjectsThinkTags(t *testing.T) {
	client := &openaiClient{
		options: openaiOptions{parseThinkTags: true},
		client:  openai.NewClient(),
	}

	// Build a prior assistant message that has both thinking and content.
	assistantMsg := message.Message{Role: message.Assistant}
	assistantMsg.AppendReasoningContent("I need to think about this carefully.")
	assistantMsg.AppendContent("The answer is 42.")

	msgs := client.convertMessages([]message.Message{assistantMsg})

	// messages[0] is the system message (empty string here); messages[1] is assistant.
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	assistantParam := msgs[1]
	if assistantParam.OfAssistant == nil {
		t.Fatal("second message is not an assistant message")
	}

	content := assistantParam.OfAssistant.Content.OfString.Value
	if !strings.HasPrefix(content, "<think>") {
		t.Errorf("expected content to start with <think>, got: %q", content)
	}
	if !strings.Contains(content, "I need to think about this carefully.") {
		t.Errorf("expected thinking content in message, got: %q", content)
	}
	if !strings.HasSuffix(content, "The answer is 42.") {
		t.Errorf("expected answer after </think>, got: %q", content)
	}
}

// TestConvertMessagesReasoningContentField verifies that without parseThinkTags
// (e.g. SiliconFlow/OpenRouter), thinking is passed as reasoning_content extra field.
func TestConvertMessagesReasoningContentField(t *testing.T) {
	client := &openaiClient{
		options: openaiOptions{parseThinkTags: false},
		client:  openai.NewClient(),
	}

	assistantMsg := message.Message{Role: message.Assistant}
	assistantMsg.AppendReasoningContent("deep reasoning here")
	assistantMsg.AppendContent("The result.")

	msgs := client.convertMessages([]message.Message{assistantMsg})

	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	assistantParam := msgs[1]
	if assistantParam.OfAssistant == nil {
		t.Fatal("second message is not an assistant message")
	}

	// Content should NOT contain <think> tags.
	content := assistantParam.OfAssistant.Content.OfString.Value
	if strings.Contains(content, "<think>") {
		t.Errorf("content should not contain <think> tags, got: %q", content)
	}
	// reasoning_content extra field should be set.
	extras := assistantParam.OfAssistant.GetExtraFields()
	if extras == nil {
		t.Fatal("expected extra fields to be set")
	}
	if _, ok := extras["reasoning_content"]; !ok {
		t.Error("expected reasoning_content in extra fields")
	}
}

func TestThinkTagParser_SimpleThinkBlock(t *testing.T) {
	p := &thinkTagParser{}
	thinking, content := p.feed("<think>I need to reason</think>The answer is 42")
	if thinking != "I need to reason" {
		t.Errorf("thinking = %q, want %q", thinking, "I need to reason")
	}
	if content != "The answer is 42" {
		t.Errorf("content = %q, want %q", content, "The answer is 42")
	}
}

func TestThinkTagParser_NoThinkTags(t *testing.T) {
	p := &thinkTagParser{}
	thinking, content := p.feed("Just regular content")
	if thinking != "" {
		t.Errorf("thinking = %q, want empty", thinking)
	}
	if content != "Just regular content" {
		t.Errorf("content = %q, want %q", content, "Just regular content")
	}
}

func TestThinkTagParser_TagSplitAcrossChunks(t *testing.T) {
	p := &thinkTagParser{}

	// Opening tag split across two chunks
	thinking1, content1 := p.feed("some text <thi")
	thinking2, content2 := p.feed("nk>reasoning here</think>answer")

	if content1 != "some text " {
		t.Errorf("content1 = %q, want %q", content1, "some text ")
	}
	if thinking1 != "" {
		t.Errorf("thinking1 = %q, want empty", thinking1)
	}
	if thinking2 != "reasoning here" {
		t.Errorf("thinking2 = %q, want %q", thinking2, "reasoning here")
	}
	if content2 != "answer" {
		t.Errorf("content2 = %q, want %q", content2, "answer")
	}
}

func TestThinkTagParser_ClosingTagSplitAcrossChunks(t *testing.T) {
	p := &thinkTagParser{}

	p.feed("<think>reasoning")
	thinking1, content1 := p.feed(" continues</thi")
	thinking2, content2 := p.feed("nk>final answer")

	combined := thinking1 + thinking2
	if combined != " continues" {
		t.Errorf("combined thinking = %q, want %q", combined, " continues")
	}
	if content1 != "" {
		t.Errorf("content1 = %q, want empty", content1)
	}
	if content2 != "final answer" {
		t.Errorf("content2 = %q, want %q", content2, "final answer")
	}
}

func TestThinkTagParser_MultipleThinkBlocks(t *testing.T) {
	p := &thinkTagParser{}
	thinking, content := p.feed("<think>first</think>middle<think>second</think>end")
	if thinking != "firstsecond" {
		t.Errorf("thinking = %q, want %q", thinking, "firstsecond")
	}
	if content != "middleend" {
		t.Errorf("content = %q, want %q", content, "middleend")
	}
}

func TestThinkTagParser_FlushIncompleteTag(t *testing.T) {
	p := &thinkTagParser{}
	_, content1 := p.feed("text <thi")
	// Stream ends without completing the tag
	_, content2 := p.flush()

	if content1 != "text " {
		t.Errorf("content1 = %q, want %q", content1, "text ")
	}
	if content2 != "<thi" {
		t.Errorf("flushed content = %q, want %q", content2, "<thi")
	}
}

func TestThinkTagParser_OllamaStreamingPattern(t *testing.T) {
	// Simulates Ollama streaming: <think> content arrives as separate chunks,
	// then </think> and finally the tool call or answer content.
	p := &thinkTagParser{}
	chunks := []string{
		"<think>",
		"Okay, I need to figure out ",
		"the weather in Toronto.",
		"</think>",
		"", // empty chunk before tool_calls (common in Ollama)
	}

	var totalThinking, totalContent string
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		t2, c2 := p.feed(chunk)
		totalThinking += t2
		totalContent += c2
	}
	t2, c2 := p.flush()
	totalThinking += t2
	totalContent += c2

	wantThinking := "Okay, I need to figure out the weather in Toronto."
	if totalThinking != wantThinking {
		t.Errorf("thinking = %q, want %q", totalThinking, wantThinking)
	}
	if totalContent != "" {
		t.Errorf("content = %q, want empty", totalContent)
	}
}

func TestExtractThinkTags(t *testing.T) {
	cases := []struct {
		input           string
		wantThinking    string
		wantContent     string
	}{
		{
			input:        "<think>I need to reason about this</think>The answer is 42.",
			wantThinking: "I need to reason about this",
			wantContent:  "The answer is 42.",
		},
		{
			input:        "No thinking here",
			wantThinking: "",
			wantContent:  "No thinking here",
		},
		{
			input:        "<think>reasoning</think>",
			wantThinking: "reasoning",
			wantContent:  "",
		},
		{
			input:        "prefix<think>think</think>suffix",
			wantThinking: "think",
			wantContent:  "prefixsuffix",
		},
	}

	for _, tc := range cases {
		thinking, content := extractThinkTags(tc.input)
		if thinking != tc.wantThinking {
			t.Errorf("input=%q: thinking = %q, want %q", tc.input, thinking, tc.wantThinking)
		}
		if content != tc.wantContent {
			t.Errorf("input=%q: content = %q, want %q", tc.input, content, tc.wantContent)
		}
	}
}

func TestFilterImageTags(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"text <image/> more", "text  more"},
		{"<image> content </image>", " content "},
		{"no images here", "no images here"},
		{"<image /> self-closing", " self-closing"},
	}
	for _, tc := range cases {
		got := filterImageTags(tc.input)
		if got != tc.want {
			t.Errorf("filterImageTags(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLongestTagPrefix(t *testing.T) {
	cases := []struct {
		s, tag string
		want   string
	}{
		{"text <thi", "<think>", "<thi"},
		{"text </thi", "</think>", "</thi"},
		{"text <", "<think>", "<"},
		{"text abc", "<think>", ""},
		{"text <think>", "<think>", ""}, // full tag present — no partial suffix
	}
	for _, tc := range cases {
		got := longestTagPrefix(tc.s, tc.tag)
		if got != tc.want {
			t.Errorf("longestTagPrefix(%q, %q) = %q, want %q", tc.s, tc.tag, got, tc.want)
		}
	}
}
