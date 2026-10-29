package agent

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// newSessionReadSource builds the tools.SessionReadSource used by the
// read-before-edit guard to recover prior reads from persisted history (for
// example after a restart). Only tool calls after the session's compaction
// point (SummaryMessageID) are returned, since earlier ones are no longer in
// the model context. Errors are returned so the guard fails closed.
func newSessionReadSource(sessions session.Service, messages message.Service) tools.SessionReadSource {
	return func(ctx context.Context, sessionID string) ([]tools.SessionToolRead, error) {
		sess, err := sessions.Get(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		msgs, err := messages.List(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if sess.SummaryMessageID != "" {
			cut := -1
			for i, m := range msgs {
				if m.ID == sess.SummaryMessageID {
					cut = i
					break
				}
			}
			if cut < 0 {
				return nil, nil // fail closed: compaction point unknown
			}
			msgs = msgs[cut+1:]
		}

		results := make(map[string]message.ToolResult)
		for _, m := range msgs {
			if m.Role != message.Tool {
				continue
			}
			for _, r := range m.ToolResults() {
				results[r.ToolCallID] = r
			}
		}

		var out []tools.SessionToolRead
		for _, m := range msgs {
			if m.Role != message.Assistant {
				continue
			}
			// messages.created_at is stored in Unix seconds (strftime('%s')).
			calledAt := time.Unix(m.CreatedAt, 0)
			for _, tc := range m.ToolCalls() {
				res, ok := results[tc.ID]
				if !ok {
					continue
				}
				out = append(out, tools.SessionToolRead{
					ToolName: tc.Name,
					Input:    tc.Input,
					Metadata: res.Metadata,
					IsError:  res.IsError,
					CalledAt: calledAt,
				})
			}
		}
		return out, nil
	}
}
