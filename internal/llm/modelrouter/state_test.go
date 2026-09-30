package modelrouter

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeState(t *testing.T, s string) state {
	t.Helper()
	var st state
	if err := json.Unmarshal([]byte(s), &st); err != nil {
		t.Fatalf("state is not valid JSON: %v\n%s", err, s)
	}
	return st
}

func TestStateTruncationBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("FIRST LINE of the prompt\n")
	for b.Len() < 80*1024 {
		b.WriteString("the quick brown fox jumps over the lazy dog, again and again\n")
	}
	b.WriteString("LAST LINE of the prompt")
	budget := 2050 - 400 // minus question/criteria overhead
	out := BuildState(Input{Prompt: b.String()}, 0, budget)

	if got, max := EstimateTokens(out), budget-SafetyMargin(budget); got > max {
		t.Fatalf("estimated %d tokens > %d", got, max)
	}
	if len(out) >= 64*1024 {
		t.Fatalf("body too large: %d", len(out))
	}
	st := decodeState(t, out)
	if !strings.HasPrefix(st.Request, "FIRST LINE of the prompt") || !strings.HasSuffix(st.Request, "LAST LINE of the prompt") {
		t.Fatalf("head/tail lost: %q ... %q", st.Request[:30], st.Request[len(st.Request)-30:])
	}
	if !strings.Contains(st.Request, "[…]") {
		t.Fatal("ellipsis marker missing")
	}

	// A huge budget is still capped below 64 KiB.
	big := BuildState(Input{Prompt: b.String()}, 0, 1_000_000)
	if len(big) >= 64*1024 {
		t.Fatalf("large-budget body %d >= 64KiB", len(big))
	}

	// A short prompt is untouched.
	short := decodeState(t, BuildState(Input{Prompt: "hello"}, 0, 2050))
	if short.Request != "hello" {
		t.Fatalf("short prompt changed: %q", short.Request)
	}
}

func TestStateHistoryPrompts(t *testing.T) {
	in := Input{Prompt: "and add tests", History: []string{"older one", "refactor the session service"}}
	st := decodeState(t, BuildState(in, 1, 2000))
	if len(st.PreviousRequests) != 1 || st.PreviousRequests[0] != "refactor the session service" {
		t.Fatalf("previous_requests = %v", st.PreviousRequests)
	}
	if st := decodeState(t, BuildState(in, 0, 2000)); len(st.PreviousRequests) != 0 {
		t.Fatalf("history must be omitted when historyPrompts=0: %v", st.PreviousRequests)
	}
	if strings.Contains(BuildState(in, 0, 2000), "previous_requests") {
		t.Fatal("previous_requests key must be absent")
	}
	long := strings.Repeat("word ", 500)
	st = decodeState(t, BuildState(Input{Prompt: "now", History: []string{long}}, 1, 2000))
	if len(st.PreviousRequests[0]) >= len(long)/2 {
		t.Fatal("history entries must be truncated harder")
	}
}

func TestStateAttachmentsNames(t *testing.T) {
	in := Input{Prompt: "what is in this image?", AttachmentNames: []string{"screenshot.png"}, HasAttachments: true}
	out := BuildState(in, 0, 2000)
	if !strings.Contains(out, "screenshot.png") {
		t.Fatalf("attachment name missing: %s", out)
	}
	if strings.Contains(out, "base64") || strings.Contains(out, "iVBOR") {
		t.Fatal("no attachment data may be sent")
	}
	if st := decodeState(t, out); len(st.Attachments) != 1 {
		t.Fatalf("attachments = %v", st.Attachments)
	}
}
