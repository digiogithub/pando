package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// SessionToolRead is one tool call from a session's persisted history paired with its result.
type SessionToolRead struct {
	ToolName string    // tool name as called by the model (may be an alias such as "read")
	Input    string    // raw JSON input of the tool call
	Metadata string    // raw metadata string of the tool result
	IsError  bool      // tool result was an error
	CalledAt time.Time // creation time of the assistant message that holds the tool call
}

// SessionReadSource returns the tool calls of a session that are still in the
// model context, i.e. only those AFTER the session's last compaction point
// (summary message). Order does not matter. The agent layer installs it.
type SessionReadSource func(ctx context.Context, sessionID string) ([]SessionToolRead, error)

var sessionReadSource atomic.Pointer[SessionReadSource]

// SetSessionReadSource installs the session history source used by the
// read-before-edit guard. nil removes it. Safe for concurrent use.
func SetSessionReadSource(src SessionReadSource) {
	if src == nil {
		sessionReadSource.Store(nil)
		return
	}
	sessionReadSource.Store(&src)
}

// lastReadTimeForEdit returns the time the file was last read, for the
// read-before-edit guard. The in-memory record wins. On a miss (e.g. after a
// server restart) it falls back to the persisted session history: the latest
// successful view of absPath is accepted only if the content it returned is
// still present in the file. The check fails closed: any doubt yields the
// zero time, and older history entries are never consulted.
//
// Only raw window content is stored in ViewResponseMetadata.Content (also for
// compressed modes). Deduplicated re-reads store no metadata, so they simply
// fail the check.
func lastReadTimeForEdit(ctx context.Context, absPath string) time.Time {
	if t := getLastReadTime(absPath); !t.IsZero() {
		return t
	}
	srcPtr := sessionReadSource.Load()
	if srcPtr == nil {
		return time.Time{}
	}
	sessionID, _ := ctx.Value(SessionIDContextKey).(string)
	if sessionID == "" {
		return time.Time{}
	}
	reads, err := (*srcPtr)(ctx, sessionID)
	if err != nil {
		logging.Debug("read guard: session history lookup failed", "sessionID", sessionID, "error", err)
		return time.Time{}
	}

	var best *SessionToolRead
	for i := range reads {
		r := &reads[i]
		if r.IsError || ResolveToolAlias(r.ToolName) != ViewToolName {
			continue
		}
		var params ViewParams
		if json.Unmarshal([]byte(r.Input), &params) != nil || params.FilePath == "" {
			continue
		}
		if resolveToolPath(params.FilePath) != absPath {
			continue
		}
		if best == nil || r.CalledAt.After(best.CalledAt) {
			best = r
		}
	}
	if best == nil {
		return time.Time{}
	}

	var meta ViewResponseMetadata
	if json.Unmarshal([]byte(best.Metadata), &meta) != nil || meta.Content == "" {
		return time.Time{}
	}
	data, err := getWorkspaceFS(ctx).ReadFile(ctx, absPath)
	if err != nil || !strings.Contains(string(data), meta.Content) {
		return time.Time{}
	}
	recordFileReadAt(absPath, best.CalledAt)
	return best.CalledAt
}
