package evaluator

import (
	"context"
	"log/slog"
	"time"

	"github.com/digiogithub/pando/internal/db"
)

const (
	defaultIdleTimeout    = 30 * time.Minute
	defaultBackfillLimit  = 50
	idleSweepLimit        = 20
	backfillPause         = 250 * time.Millisecond
	backgroundStartDelay  = 30 * time.Second
	minSweepInterval      = time.Minute
	maxSweepInterval      = 5 * time.Minute
	sweepEvaluationBudget = 2 * time.Minute
)

// SweepOptions controls one background sweep over unevaluated sessions.
type SweepOptions struct {
	// IdleFor is how long a session must have been idle to be picked up.
	IdleFor time.Duration
	// Limit bounds the number of sessions evaluated in this sweep.
	Limit int
	// SkipJudge disables the LLM judge for the sweep.
	SkipJudge bool
	// Pause is slept between evaluations to rate-limit the sweep.
	Pause time.Duration
	// OnResult, when set, receives every successfully scored session.
	OnResult func(*Result)
}

// idleTimeout returns the configured idle timeout, falling back to the default.
func (s *EvaluatorService) idleTimeout() time.Duration {
	if d, err := time.ParseDuration(s.cfg.IdleTimeout); err == nil && d > 0 {
		return d
	}
	return defaultIdleTimeout
}

// Sweep evaluates sessions that have no score, at least two user messages and
// have been idle for opts.IdleFor, oldest first. It returns how many sessions
// were scored. Sessions that fail or are skipped are not retried by later
// sweeps of this process, so a bad session cannot starve the queue.
func (s *EvaluatorService) Sweep(ctx context.Context, opts SweepOptions) (int, error) {
	if s == nil || !s.cfg.Enabled || opts.Limit <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-opts.IdleFor).Unix()
	include := int64(0)
	if s.cfg.IncludeSubagents {
		include = 1
	}
	// Over-fetch so sessions already known to fail do not eat the limit.
	ids, err := s.listUnscored(ctx, cutoff, include, int64(opts.Limit)+s.failedCount())
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if done >= opts.Limit || ctx.Err() != nil {
			break
		}
		if _, bad := s.failed.Load(id); bad {
			continue
		}
		evalCtx, cancel := context.WithTimeout(ctx, sweepEvaluationBudget)
		res, err := s.EvaluateNow(evalCtx, id, EvaluateOptions{SkipJudge: opts.SkipJudge})
		cancel()
		switch {
		case err != nil:
			slog.Warn("evaluator: sweep evaluation failed", "session_id", id, "err", err)
			s.failed.Store(id, struct{}{})
		case res.Skipped != "":
			s.failed.Store(id, struct{}{})
		default:
			done++
			if opts.OnResult != nil {
				opts.OnResult(res)
			}
		}
		if opts.Pause > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(opts.Pause):
			}
		}
	}
	return done, nil
}

func (s *EvaluatorService) failedCount() int64 {
	var n int64
	s.failed.Range(func(_, _ any) bool { n++; return true })
	return n
}

// BackgroundState describes the idle sweeper / startup backfill of this process.
type BackgroundState struct {
	// Started is true once RunBackground was called (the goroutine exists).
	Started bool `json:"started"`
	// Primary is whether this instance owned the DB writer at the last tick.
	Primary bool `json:"primary"`
	// BackfillDone is true once the one-shot startup backfill ran.
	BackfillDone bool `json:"backfill_done"`
	// BackfillEvaluated is how many sessions the startup backfill scored.
	BackfillEvaluated int `json:"backfill_evaluated"`
	// LastSweepAt is the unix time of the last idle sweep (0 = none yet).
	LastSweepAt int64 `json:"last_sweep_at"`
	// LastSweepEvaluated is how many sessions the last idle sweep scored.
	LastSweepEvaluated int `json:"last_sweep_evaluated"`
}

// BackgroundState returns a snapshot of the background worker state.
func (s *EvaluatorService) BackgroundState() BackgroundState {
	if s == nil {
		return BackgroundState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bg
}

// SetFirstPassHook registers fn to run once, after the first background pass on
// the primary instance (startup backfill + idle sweep). Call before RunBackground.
func (s *EvaluatorService) SetFirstPassHook(fn func(ctx context.Context)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onFirstPass = fn
	s.mu.Unlock()
}

func (s *EvaluatorService) updateBackground(fn func(*BackgroundState)) {
	s.mu.Lock()
	fn(&s.bg)
	s.mu.Unlock()
}

// RunBackground starts the idle sweeper and the one-shot startup backfill. It
// blocks until ctx is done, so call it in a goroutine. isPrimary is consulted
// before every action so the work only runs on the instance that owns the
// database writer, including after a failover promotion.
func (s *EvaluatorService) RunBackground(ctx context.Context, isPrimary func() bool) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	idle := s.idleTimeout()
	interval := idle / 2
	if interval < minSweepInterval {
		interval = minSweepInterval
	}
	if interval > maxSweepInterval {
		interval = maxSweepInterval
	}

	s.updateBackground(func(b *BackgroundState) { b.Started = true })
	select {
	case <-ctx.Done():
		return
	case <-time.After(backgroundStartDelay):
	}

	backfilled := s.cfg.BackfillLimit < 0
	diagnosed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		primary := isPrimary()
		s.updateBackground(func(b *BackgroundState) { b.Primary = primary })
		if primary {
			if !backfilled {
				backfilled = true
				limit := s.cfg.BackfillLimit
				if limit == 0 {
					limit = defaultBackfillLimit
				}
				n, err := s.Sweep(ctx, SweepOptions{
					IdleFor:   idle,
					Limit:     limit,
					SkipJudge: !s.cfg.BackfillJudge,
					Pause:     backfillPause,
				})
				slog.Info("evaluator: startup backfill finished", "evaluated", n, "err", err)
				s.updateBackground(func(b *BackgroundState) { b.BackfillDone, b.BackfillEvaluated = true, n })
			}
			if n, err := s.Sweep(ctx, SweepOptions{IdleFor: idle, Limit: idleSweepLimit, Pause: backfillPause}); err != nil {
				slog.Warn("evaluator: idle sweep failed", "err", err)
				s.recordEvalError(err)
			} else {
				s.updateBackground(func(b *BackgroundState) { b.LastSweepAt, b.LastSweepEvaluated = time.Now().Unix(), n })
				if n > 0 {
					slog.Info("evaluator: idle sweep evaluated sessions", "evaluated", n)
				}
			}
			if !diagnosed {
				diagnosed = true
				s.mu.Lock()
				hook := s.onFirstPass
				s.mu.Unlock()
				if hook != nil {
					hook(ctx)
				}
			}
		} else {
			slog.Debug("evaluator: background sweep skipped, not primary")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *EvaluatorService) listUnscored(ctx context.Context, cutoff, includeChildren, limit int64) ([]string, error) {
	return s.db.ListUnscoredSessions(ctx, db.ListUnscoredSessionsParams{
		UpdatedAt: cutoff,
		Column2:   includeChildren,
		Limit:     limit,
	})
}
