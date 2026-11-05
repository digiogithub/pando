package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/message"
)

// ephemeralUserMessagesKey carries the run-scoped data messages attached with
// WithEphemeralUserMessages.
type ephemeralUserMessagesKey struct{}

// ephemeralMessageIDPrefix marks the in-memory ids of ephemeral messages. They
// never reach the message store, so the id only has to be unique within one
// request history.
const ephemeralMessageIDPrefix = "ephemeral-"

// WithEphemeralUserMessages returns a context that makes Service.Run deliver
// each non-empty text to the model as its own user-role message, placed
// immediately before the run's user message.
//
// Ephemeral messages are request material, not conversation: they are never
// written to the session's message store, so they do not appear in the
// transcript, are not summarised by compaction and are not replayed on later
// runs. A caller that wants the model to keep seeing them supplies them again
// on every run. They stay in the in-memory history for every iteration of the
// run they were attached to (tool calls, continuations, steering), and are
// re-inserted when that history is reloaded after a mid-run compaction.
//
// Callers use this for ambient data the client application attaches to a
// request (page state, selected records) that must reach the model without
// being merged into, or stored as, the user's own words. The text is sent as
// is; framing it as data (an envelope, a statement of origin) is the caller's
// responsibility.
func WithEphemeralUserMessages(ctx context.Context, texts ...string) context.Context {
	var kept []string
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ephemeralUserMessagesKey{}, kept)
}

// EphemeralUserMessages returns the texts attached to ctx by
// WithEphemeralUserMessages, in order, or nil. Service implementations other
// than the built-in agent (test doubles, proxies) use it to honour or inspect
// them.
func EphemeralUserMessages(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	texts, _ := ctx.Value(ephemeralUserMessagesKey{}).([]string)
	return append([]string(nil), texts...)
}

// ephemeralUserMessages builds the in-memory user messages attached to ctx by
// WithEphemeralUserMessages. It returns nil when there are none.
func ephemeralUserMessages(ctx context.Context, sessionID string) []message.Message {
	texts := EphemeralUserMessages(ctx)
	if len(texts) == 0 {
		return nil
	}
	now := time.Now().Unix()
	out := make([]message.Message, 0, len(texts))
	for i, t := range texts {
		out = append(out, message.Message{
			ID:        fmt.Sprintf("%s%d", ephemeralMessageIDPrefix, i),
			Role:      message.User,
			SessionID: sessionID,
			Parts:     []message.ContentPart{message.TextContent{Text: t}},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return out
}

// insertBeforeLastUser returns a new history with extra inserted immediately
// before the last user-role message of history, which is the turn the extra
// messages accompany. With no user message in history they go first, the only
// other position every provider accepts for a user-role message. history is
// not modified.
func insertBeforeLastUser(history, extra []message.Message) []message.Message {
	if len(extra) == 0 {
		return history
	}
	at := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == message.User {
			at = i
			break
		}
	}
	out := make([]message.Message, 0, len(history)+len(extra))
	out = append(out, history[:at]...)
	out = append(out, extra...)
	out = append(out, history[at:]...)
	return out
}

// withEphemeralMessages inserts the ephemeral messages attached to ctx before
// the last user message of history (see WithEphemeralUserMessages).
func withEphemeralMessages(ctx context.Context, sessionID string, history []message.Message) []message.Message {
	return insertBeforeLastUser(history, ephemeralUserMessages(ctx, sessionID))
}
