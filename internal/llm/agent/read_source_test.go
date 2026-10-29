package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

type readSrcSessions struct {
	session.Service
	sess session.Session
	err  error
}

func (s *readSrcSessions) Get(context.Context, string) (session.Session, error) {
	return s.sess, s.err
}

type readSrcMessages struct {
	message.Service
	msgs []message.Message
}

func (m *readSrcMessages) List(context.Context, string) ([]message.Message, error) {
	return m.msgs, nil
}

func readSrcCall(id string, created int64, callID, input string) message.Message {
	return message.Message{ID: id, Role: message.Assistant, CreatedAt: created, Parts: []message.ContentPart{
		message.ToolCall{ID: callID, Name: "view", Input: input},
	}}
}

func readSrcResult(id, callID, meta string, isErr bool) message.Message {
	return message.Message{ID: id, Role: message.Tool, Parts: []message.ContentPart{
		message.ToolResult{ToolCallID: callID, Metadata: meta, IsError: isErr},
	}}
}

func TestSessionReadSource(t *testing.T) {
	msgs := []message.Message{
		readSrcCall("a1", 100, "c1", `{"file_path":"old.go"}`),
		readSrcResult("t1", "c1", "m1", false),
		{ID: "sum", Role: message.Assistant, CreatedAt: 150},
		readSrcCall("a2", 1700000000, "c2", `{"file_path":"new.go"}`),
		readSrcResult("t2", "c2", "meta2", true),
		readSrcCall("a3", 1700000100, "c3", `{"file_path":"nores.go"}`),
	}
	ctx := context.Background()

	t.Run("only after summary, paired, seconds", func(t *testing.T) {
		src := newSessionReadSource(
			&readSrcSessions{sess: session.Session{SummaryMessageID: "sum"}},
			&readSrcMessages{msgs: msgs})
		got, err := src(ctx, "s")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 entry, got %d: %+v", len(got), got)
		}
		g := got[0]
		if g.Input != `{"file_path":"new.go"}` || g.Metadata != "meta2" || !g.IsError || g.ToolName != "view" {
			t.Fatalf("bad entry: %+v", g)
		}
		if !g.CalledAt.Equal(time.Unix(1700000000, 0)) {
			t.Fatalf("bad CalledAt: %v", g.CalledAt)
		}
	})

	t.Run("no summary returns all paired", func(t *testing.T) {
		src := newSessionReadSource(&readSrcSessions{}, &readSrcMessages{msgs: msgs})
		got, err := src(ctx, "s")
		if err != nil || len(got) != 2 {
			t.Fatalf("want 2 entries, got %d err=%v", len(got), err)
		}
	})

	t.Run("summary id missing fails closed", func(t *testing.T) {
		src := newSessionReadSource(
			&readSrcSessions{sess: session.Session{SummaryMessageID: "gone"}},
			&readSrcMessages{msgs: msgs})
		got, err := src(ctx, "s")
		if err != nil || len(got) != 0 {
			t.Fatalf("want none, got %d err=%v", len(got), err)
		}
	})

	t.Run("session error propagates", func(t *testing.T) {
		src := newSessionReadSource(&readSrcSessions{err: errors.New("boom")}, &readSrcMessages{msgs: msgs})
		if _, err := src(ctx, "s"); err == nil {
			t.Fatal("expected error")
		}
	})
}
