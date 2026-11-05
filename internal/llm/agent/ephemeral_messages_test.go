package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
)

func textMsg(role message.MessageRole, text string) message.Message {
	return message.Message{Role: role, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

func roleTexts(history []message.Message) []string {
	out := make([]string, 0, len(history))
	for _, m := range history {
		out = append(out, string(m.Role)+":"+m.Content().String())
	}
	return out
}

func TestWithEphemeralUserMessagesDropsBlankTexts(t *testing.T) {
	ctx := context.Background()
	if got := WithEphemeralUserMessages(ctx, "", "  \n"); got != ctx {
		t.Fatal("blank texts must leave the context untouched")
	}
	got := EphemeralUserMessages(WithEphemeralUserMessages(ctx, "a", "", "b"))
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("EphemeralUserMessages = %v, want [a b]", got)
	}
	if EphemeralUserMessages(ctx) != nil {
		t.Fatal("a plain context carries no ephemeral messages")
	}
}

func TestInsertBeforeLastUserPlacesDataBeforeTheTurn(t *testing.T) {
	history := []message.Message{
		textMsg(message.User, "q1"),
		textMsg(message.Assistant, "a1"),
		textMsg(message.User, "q2"),
	}
	got := roleTexts(insertBeforeLastUser(history, []message.Message{textMsg(message.User, "data")}))
	want := "user:q1|assistant:a1|user:data|user:q2"
	if strings.Join(got, "|") != want {
		t.Fatalf("history = %v, want %s", got, want)
	}
	if len(history) != 3 {
		t.Fatal("the input history must not be modified")
	}

	// No user message (e.g. a reload whose only message is an assistant one):
	// the data goes first.
	got = roleTexts(insertBeforeLastUser([]message.Message{textMsg(message.Assistant, "a")}, []message.Message{textMsg(message.User, "data")}))
	if strings.Join(got, "|") != "user:data|assistant:a" {
		t.Fatalf("history = %v, want the data first", got)
	}
}

// TestEphemeralMessagesReachTheModelButNotTheStore is the history contract of
// WithEphemeralUserMessages: the data message is sent right before the user's
// message on every request of its run (tool iterations included), the stored
// user message is exactly the user's text, nothing of the data is stored, and
// a later run without it does not see it again.
func TestEphemeralMessagesReachTheModelButNotTheStore(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(c *config.ModelAutoModeConfig) { c.Enabled = false }))
	prov := env.provs[autoCoder]
	prov.respond = func(call int, _ []message.Message) []provider.ProviderEvent {
		if call == 0 {
			return toolReply("call-1", "echo")
		}
		return textReply("done")
	}
	sessionID := env.session("s-ephemeral", autoCoder)
	const data = "<context_data>{\"page\":\"board\"}</context_data>"

	ctx := WithEphemeralUserMessages(context.Background(), data)
	ch, err := env.a.Run(ctx, sessionID, "what is on this page?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	drainAutoEvents(t, ch)

	if got := prov.callCount(); got != 2 {
		t.Fatalf("expected a tool iteration and a final request, got %d calls", got)
	}
	for call := 0; call < 2; call++ {
		h := prov.history(call)
		idx := -1
		for i, m := range h {
			if m.Content().String() == data {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("call %d: the data message was not sent: %v", call, roleTexts(h))
		}
		if h[idx].Role != message.User {
			t.Fatalf("call %d: data message role = %s, want user", call, h[idx].Role)
		}
		if idx+1 >= len(h) || h[idx+1].Role != message.User || h[idx+1].Content().String() != "what is on this page?" {
			t.Fatalf("call %d: the data message must immediately precede the user's message: %v", call, roleTexts(h))
		}
	}

	stored, _ := env.msgs.List(context.Background(), sessionID)
	var users []string
	for _, m := range stored {
		if strings.Contains(m.Content().String(), "context_data") {
			t.Fatalf("the data message was persisted: %+v", m)
		}
		if m.Role == message.User {
			users = append(users, m.Content().String())
		}
	}
	if users[len(users)-1] != "what is on this page?" {
		t.Fatalf("stored user message = %q, want exactly the user's text", users[len(users)-1])
	}

	// A later run without context does not see the earlier data.
	prov.respond = nil
	env.run(sessionID, "and now?")
	last := prov.history(prov.callCount() - 1)
	for _, m := range last {
		if strings.Contains(m.Content().String(), "context_data") {
			t.Fatalf("a later run replayed the earlier data message: %v", roleTexts(last))
		}
	}
}

// TestReloadAfterCompactionKeepsEphemeralMessages covers the mid-run reload
// paths (auto-compaction, context-length recovery): the reloaded history is
// rebuilt from the store, which never holds the data message, so the reload
// must put it back in front of the turn.
func TestReloadAfterCompactionKeepsEphemeralMessages(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(c *config.ModelAutoModeConfig) { c.Enabled = false }))
	sessionID := env.session("s-ephemeral-reload", autoCoder)
	_, _ = env.msgs.Create(context.Background(), sessionID, message.CreateMessageParams{
		Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "the turn"}},
	})

	ctx := WithEphemeralUserMessages(context.Background(), "data")
	reloaded, err := env.a.loadSessionMessagesFromSummary(ctx, sessionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := strings.Join(roleTexts(reloaded), "|")
	if !strings.HasSuffix(got, "user:data|user:the turn") {
		t.Fatalf("reloaded history = %s, want the data message right before the turn", got)
	}

	plain, err := env.a.loadSessionMessagesFromSummary(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if strings.Contains(strings.Join(roleTexts(plain), "|"), "user:data") {
		t.Fatal("a reload without ephemeral messages must not invent one")
	}
}
