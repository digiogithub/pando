package app

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
)

// cleanupLeftoverEnrichmentSessions removes any "ctxenrich-*" session (and its
// indexed remembrances events) left over by a version of the context-enrichment
// agent loop that never deleted its own sessions — see
// [[pando/fixes/context_enricher_agent_loop_first_event.md]]. The loop now
// deletes its session as soon as each run finishes (see
// agentLoopEnricher.deleteSessionCleanup), so this only ever has work to do
// once, cleaning up whatever accumulated before that fix shipped.
//
// It is safe to run on every startup: listing sessions and deleting a handful
// of matches is cheap, and once the backlog is gone it is a single List call
// that finds nothing. Called only from the instance that owns the database
// directly (see the call site in New): a secondary would have to proxy every
// one of these calls over IPC for rows the primary has usually already removed,
// so it is skipped there rather than duplicated.
func (app *App) cleanupLeftoverEnrichmentSessions(ctx context.Context) {
	if app.Sessions == nil {
		return
	}
	sessions, err := app.Sessions.List(ctx)
	if err != nil {
		logging.Debug("startup cleanup: failed to list sessions", "error", err)
		return
	}

	removed := 0
	for _, sess := range sessions {
		if !strings.HasPrefix(sess.ID, ctxEnrichSessionIDPrefix) {
			continue
		}
		if err := app.Sessions.Delete(ctx, sess.ID); err != nil {
			logging.Debug("startup cleanup: failed to delete leftover enrichment session",
				"session_id", sess.ID, "error", err)
			continue
		}
		if app.Remembrances != nil && app.Remembrances.Events != nil {
			if err := app.Remembrances.Events.ReplaceSessionEvents(ctx, sess.ID, sessionIndexSubject, nil, nil, nil); err != nil {
				logging.Debug("startup cleanup: failed to remove indexed events for leftover session",
					"session_id", sess.ID, "error", err)
			}
		}
		removed++
	}
	if removed > 0 {
		logging.Info("startup cleanup: removed leftover context-enrichment sessions", "count", removed)
	}
}
