// Package breaker implements the delegated-task circuit breaker and respawn
// guard: the anti-thrash layer that decides whether a task may be re-executed.
//
// Two independent mechanisms live here:
//
//   - The circuit breaker counts consecutive failed executions of a task
//     (accumulating across retry chains) and trips once the limit is reached, so
//     an unfixable task is parked instead of respawned forever.
//   - The respawn guard defers — rather than rejects — a re-execution that is
//     very unlikely to help right now: a quota wall that has not cooled down, an
//     authentication blocker that needs a human, or a task that just succeeded.
//
// The package is deliberately dependency-free (models + stdlib) so it can be
// unit-tested in isolation and reused by any respawn path (Relaunch, Retry, and
// future automatic recovery loops).
package breaker

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// FailureKind classifies why a task's last execution failed. It is persisted on
// the task (Task.LastFailureKind) so the guard can reason about the failure
// without re-parsing output.
type FailureKind string

const (
	// FailureKindNone means no failure has been recorded (or it was cleared by a
	// subsequent success).
	FailureKindNone FailureKind = ""
	// FailureKindRateLimit marks a quota / rate-limit wall. Retrying immediately
	// just burns the remaining budget; the guard defers until the cooldown ends.
	FailureKindRateLimit FailureKind = "rate_limit"
	// FailureKindAuth marks an authentication / authorization blocker. No amount
	// of retrying fixes it — a human must supply credentials.
	FailureKindAuth FailureKind = "auth"
	// FailureKindOther is any other failure: retrying may well help.
	FailureKindOther FailureKind = "other"
)

// Reason identifies why a respawn was denied. It is stable, machine-readable
// text suitable for logs, tool results and UI badges.
type Reason string

const (
	// ReasonNone is the reason of an allowed decision.
	ReasonNone Reason = ""
	// ReasonBreakerTripped means the consecutive-failure limit was reached.
	ReasonBreakerTripped Reason = "breaker_tripped"
	// ReasonRateLimitCooldown means the last run hit a quota wall and the
	// cooldown has not elapsed.
	ReasonRateLimitCooldown Reason = "rate_limit_cooldown"
	// ReasonBlockerAuth means the last run failed on authentication.
	ReasonBlockerAuth Reason = "blocker_auth"
	// ReasonRecentSuccess means the task succeeded within the recent-success
	// window, so re-running it is probably redundant.
	ReasonRecentSuccess Reason = "recent_success"
)

// Documented defaults, shared with the config layer.
const (
	// DefaultMaxRetries is the number of consecutive failures tolerated before
	// the breaker trips.
	DefaultMaxRetries = 3
	// DefaultRateLimitCooldown is how long a quota-walled task is deferred.
	DefaultRateLimitCooldown = 5 * time.Minute
	// DefaultRecentSuccessWindow is how recently a task must have succeeded for
	// a re-run to be considered redundant.
	DefaultRecentSuccessWindow = 2 * time.Minute
)

// Options carries the guard's tunables. A zero Options is usable: the zero
// values disable each check individually, so an unconfigured guard allows
// everything.
type Options struct {
	// Disabled short-circuits every check: Guard always allows. Mirrors the
	// config's opt-out.
	Disabled bool
	// MaxRetries is the consecutive-failure limit. 0 disables the breaker.
	MaxRetries int
	// RateLimitCooldown is how long a rate-limited task is deferred after its
	// last failure. 0 disables the check.
	RateLimitCooldown time.Duration
	// RecentSuccessWindow is how long after a success a re-run is considered
	// redundant. 0 disables the check.
	RecentSuccessWindow time.Duration
}

// Decision is the guard's verdict for one respawn attempt.
type Decision struct {
	// Allow is true when the task may be re-executed.
	Allow bool
	// Reason is the machine-readable denial reason ("" when allowed).
	Reason Reason
	// Detail is a human-readable explanation, safe to surface to the model.
	Detail string
	// RetryAfter is how long to wait before the denial would clear on its own.
	// Zero means the denial does not clear with time (needs a human, a fix, or
	// an explicit force).
	RetryAfter time.Duration
}

// Error renders the decision as an error message. It returns "" when allowed.
func (d Decision) Error() string {
	if d.Allow {
		return ""
	}
	msg := fmt.Sprintf("respawn refused (%s): %s", d.Reason, d.Detail)
	if d.RetryAfter > 0 {
		msg += fmt.Sprintf(" — retry after %s", d.RetryAfter.Round(time.Second))
	}
	return msg + ". Pass force to override."
}

// allowed is the canonical allowing decision.
var allowed = Decision{Allow: true}

// Guard decides whether task may be re-executed at time now.
//
// Checks run in order of severity: breaker (needs a fix), auth blocker (needs a
// human), rate-limit cooldown (needs time), recent success (probably
// redundant). A nil task is always allowed — the guard never invents failures.
func Guard(task *models.Task, opts Options, now time.Time) Decision {
	if task == nil || opts.Disabled {
		return allowed
	}

	limit := opts.MaxRetries
	if task.MaxRetries > 0 {
		limit = task.MaxRetries
	}
	if limit > 0 && task.ConsecutiveFailures >= limit {
		return Decision{
			Reason: ReasonBreakerTripped,
			Detail: fmt.Sprintf("%d consecutive failures reached the limit of %d; the task needs a fix (new prompt, engine or model), not another identical run",
				task.ConsecutiveFailures, limit),
		}
	}

	switch FailureKind(task.LastFailureKind) {
	case FailureKindAuth:
		return Decision{
			Reason: ReasonBlockerAuth,
			Detail: "the last run failed on authentication/authorization; retrying cannot fix it until credentials are repaired",
		}
	case FailureKindRateLimit:
		if opts.RateLimitCooldown > 0 && task.LastFailureAt != nil {
			elapsed := now.Sub(*task.LastFailureAt)
			if elapsed < opts.RateLimitCooldown {
				return Decision{
					Reason:     ReasonRateLimitCooldown,
					Detail:     "the last run hit a rate limit / quota wall and the cooldown has not elapsed",
					RetryAfter: opts.RateLimitCooldown - elapsed,
				}
			}
		}
	}

	// Recent success only guards a task that is currently sitting on a success;
	// a task that has failed since is always allowed through this check.
	if opts.RecentSuccessWindow > 0 && task.Status == models.TaskStatusCompleted && task.LastSuccessAt != nil {
		elapsed := now.Sub(*task.LastSuccessAt)
		if elapsed < opts.RecentSuccessWindow {
			return Decision{
				Reason:     ReasonRecentSuccess,
				Detail:     "the task completed successfully moments ago; re-running it is probably redundant",
				RetryAfter: opts.RecentSuccessWindow - elapsed,
			}
		}
	}

	return allowed
}

// RecordOutcome updates the task's breaker state from a terminal execution.
// A success resets the counter and clears the failure classification; a failure
// increments the counter and records what kind of failure it was. Non-terminal
// or cancelled tasks are left untouched: a user-initiated cancel is not a
// failure of the task.
//
// It returns true when this outcome tripped the breaker (the failure that
// reached the limit), so the caller can log/emit the event exactly once.
func RecordOutcome(task *models.Task, opts Options, now time.Time) bool {
	if task == nil {
		return false
	}

	switch task.Status {
	case models.TaskStatusCompleted:
		task.ConsecutiveFailures = 0
		task.LastFailureKind = string(FailureKindNone)
		task.LastFailureAt = nil
		t := now
		task.LastSuccessAt = &t
		return false
	case models.TaskStatusFailed:
		task.ConsecutiveFailures++
		task.LastFailureKind = string(Classify(FailureText(task)))
		t := now
		task.LastFailureAt = &t
		limit := opts.MaxRetries
		if task.MaxRetries > 0 {
			limit = task.MaxRetries
		}
		return limit > 0 && task.ConsecutiveFailures >= limit
	default:
		return false
	}
}

// FailureText assembles the text the classifier inspects for a failed task,
// preferring the explicit error fields over raw output.
func FailureText(task *models.Task) string {
	if task == nil {
		return ""
	}
	parts := []string{task.Error, task.RawError}
	if task.Conclusion != nil {
		parts = append(parts, task.Conclusion.Summary)
	}
	parts = append(parts, task.OutputTail)
	return strings.Join(parts, "\n")
}

// Patterns used to classify a failure. They are intentionally broad — a
// misclassification only changes how long a respawn is deferred, never whether
// the task's result is kept.
var (
	rateLimitPattern = regexp.MustCompile(`(?i)rate[ _-]?limit|too many requests|\b429\b|quota (exceeded|exhausted)|insufficient[ _-]?quota|overloaded|capacity (exceeded|constraint)|usage limit|out of credits|billing hard limit`)
	authPattern      = regexp.MustCompile(`(?i)\b401\b|\b403\b|unauthori[sz]ed|forbidden|authentication (failed|error|required)|invalid[ _-]?api[ _-]?key|missing[ _-]?api[ _-]?key|permission denied|invalid credentials|token (expired|invalid)|not logged in|please (log ?in|authenticate)`)
)

// Classify maps free-form failure text to a FailureKind. Empty text yields
// FailureKindOther: something failed, we just do not know what.
//
// Rate-limit wins over auth when both match: a 429 accompanied by an auth-ish
// word is far more likely a quota wall, and deferring is the safer response.
func Classify(text string) FailureKind {
	if strings.TrimSpace(text) == "" {
		return FailureKindOther
	}
	if rateLimitPattern.MatchString(text) {
		return FailureKindRateLimit
	}
	if authPattern.MatchString(text) {
		return FailureKindAuth
	}
	return FailureKindOther
}
