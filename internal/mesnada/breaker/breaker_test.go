package breaker

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		text string
		want FailureKind
	}{
		{"empty is other", "   ", FailureKindOther},
		{"429", "request failed with status 429", FailureKindRateLimit},
		{"rate limit words", "Rate limit exceeded, please slow down", FailureKindRateLimit},
		{"quota", "quota exceeded for this project", FailureKindRateLimit},
		{"overloaded", "Error: server overloaded", FailureKindRateLimit},
		{"401", "HTTP 401 returned by provider", FailureKindAuth},
		{"invalid api key", "invalid api key supplied", FailureKindAuth},
		{"not logged in", "you are not logged in; run login first", FailureKindAuth},
		{"generic", "panic: nil pointer dereference", FailureKindOther},
		{"rate limit wins over auth", "401 unauthorized: rate limit exceeded", FailureKindRateLimit},
	}
	for _, tc := range cases {
		if got := Classify(tc.text); got != tc.want {
			t.Errorf("%s: Classify(%q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}

func TestGuardAllowsCleanTask(t *testing.T) {
	opts := Options{MaxRetries: 3, RateLimitCooldown: time.Minute, RecentSuccessWindow: time.Minute}

	if d := Guard(nil, opts, time.Now()); !d.Allow {
		t.Error("nil task must be allowed")
	}
	if d := Guard(&models.Task{ID: "t"}, opts, time.Now()); !d.Allow {
		t.Error("fresh task must be allowed")
	}
	// Disabled short-circuits even a tripped breaker.
	tripped := &models.Task{ID: "t", ConsecutiveFailures: 99}
	if d := Guard(tripped, Options{Disabled: true, MaxRetries: 3}, time.Now()); !d.Allow {
		t.Error("disabled guard must allow everything")
	}
}

func TestGuardBreakerTrips(t *testing.T) {
	opts := Options{MaxRetries: 3}

	task := &models.Task{ID: "t", ConsecutiveFailures: 2}
	if d := Guard(task, opts, time.Now()); !d.Allow {
		t.Fatal("below the limit must be allowed")
	}

	task.ConsecutiveFailures = 3
	d := Guard(task, opts, time.Now())
	if d.Allow || d.Reason != ReasonBreakerTripped {
		t.Fatalf("at the limit must trip: allow=%v reason=%q", d.Allow, d.Reason)
	}
	if d.RetryAfter != 0 {
		t.Error("a tripped breaker does not clear with time")
	}

	// A per-task override beats the configured limit.
	task.MaxRetries = 10
	if d := Guard(task, opts, time.Now()); !d.Allow {
		t.Error("per-task MaxRetries override must raise the limit")
	}

	// A zero limit disables the breaker entirely.
	task.MaxRetries = 0
	if d := Guard(task, Options{}, time.Now()); !d.Allow {
		t.Error("MaxRetries=0 must disable the breaker")
	}
}

func TestGuardAuthBlockerNeverClears(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	task := &models.Task{
		ID:              "t",
		LastFailureKind: string(FailureKindAuth),
		LastFailureAt:   &past,
	}
	d := Guard(task, Options{MaxRetries: 3, RateLimitCooldown: time.Minute}, time.Now())
	if d.Allow || d.Reason != ReasonBlockerAuth {
		t.Fatalf("auth failure must be deferred: allow=%v reason=%q", d.Allow, d.Reason)
	}
	if d.RetryAfter != 0 {
		t.Error("an auth blocker needs a human, not time")
	}
}

func TestGuardRateLimitCooldown(t *testing.T) {
	now := time.Now()
	failedAt := now.Add(-2 * time.Minute)
	task := &models.Task{
		ID:              "t",
		LastFailureKind: string(FailureKindRateLimit),
		LastFailureAt:   &failedAt,
	}
	opts := Options{MaxRetries: 3, RateLimitCooldown: 5 * time.Minute}

	d := Guard(task, opts, now)
	if d.Allow || d.Reason != ReasonRateLimitCooldown {
		t.Fatalf("inside the cooldown must be deferred: allow=%v reason=%q", d.Allow, d.Reason)
	}
	if want := 3 * time.Minute; d.RetryAfter != want {
		t.Errorf("RetryAfter = %s, want %s", d.RetryAfter, want)
	}

	// Past the cooldown the task is allowed again.
	if d := Guard(task, opts, now.Add(4*time.Minute)); !d.Allow {
		t.Error("expired cooldown must allow the respawn")
	}
	// A zero cooldown disables the check.
	if d := Guard(task, Options{MaxRetries: 3}, now); !d.Allow {
		t.Error("RateLimitCooldown=0 must disable the check")
	}
}

func TestGuardRecentSuccessOnlyForCompletedTasks(t *testing.T) {
	now := time.Now()
	succeededAt := now.Add(-30 * time.Second)
	opts := Options{MaxRetries: 3, RecentSuccessWindow: 2 * time.Minute}

	task := &models.Task{ID: "t", Status: models.TaskStatusCompleted, LastSuccessAt: &succeededAt}
	d := Guard(task, opts, now)
	if d.Allow || d.Reason != ReasonRecentSuccess {
		t.Fatalf("a just-succeeded task must be deferred: allow=%v reason=%q", d.Allow, d.Reason)
	}
	if want := 90 * time.Second; d.RetryAfter != want {
		t.Errorf("RetryAfter = %s, want %s", d.RetryAfter, want)
	}

	// The same success timestamp on a task that has since failed is irrelevant.
	task.Status = models.TaskStatusFailed
	if d := Guard(task, opts, now); !d.Allow {
		t.Error("a failed task must not be blocked by an older success")
	}
}

func TestRecordOutcome(t *testing.T) {
	now := time.Now()
	opts := Options{MaxRetries: 2}

	// A failure increments and classifies.
	task := &models.Task{ID: "t", Status: models.TaskStatusFailed, Error: "HTTP 429 rate limit"}
	if tripped := RecordOutcome(task, opts, now); tripped {
		t.Error("first failure must not trip a limit of 2")
	}
	if task.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", task.ConsecutiveFailures)
	}
	if task.LastFailureKind != string(FailureKindRateLimit) {
		t.Errorf("LastFailureKind = %q, want rate_limit", task.LastFailureKind)
	}
	if task.LastFailureAt == nil {
		t.Fatal("LastFailureAt must be recorded")
	}

	// The second failure reaches the limit.
	if tripped := RecordOutcome(task, opts, now); !tripped {
		t.Error("second failure must trip a limit of 2")
	}

	// A success resets everything.
	task.Status = models.TaskStatusCompleted
	if tripped := RecordOutcome(task, opts, now); tripped {
		t.Error("a success never trips the breaker")
	}
	if task.ConsecutiveFailures != 0 || task.LastFailureKind != "" || task.LastFailureAt != nil {
		t.Errorf("success must reset breaker state, got failures=%d kind=%q at=%v",
			task.ConsecutiveFailures, task.LastFailureKind, task.LastFailureAt)
	}
	if task.LastSuccessAt == nil {
		t.Error("LastSuccessAt must be recorded")
	}
}

func TestRecordOutcomeIgnoresCancelAndNonTerminal(t *testing.T) {
	opts := Options{MaxRetries: 2}
	for _, status := range []models.TaskStatus{
		models.TaskStatusCancelled,
		models.TaskStatusRunning,
		models.TaskStatusPending,
		models.TaskStatusPaused,
	} {
		task := &models.Task{ID: "t", Status: status, ConsecutiveFailures: 1}
		RecordOutcome(task, opts, time.Now())
		if task.ConsecutiveFailures != 1 {
			t.Errorf("status %s must leave the breaker untouched, got %d", status, task.ConsecutiveFailures)
		}
	}
	if RecordOutcome(nil, opts, time.Now()) {
		t.Error("nil task must be a no-op")
	}
}

func TestDecisionError(t *testing.T) {
	if msg := (Decision{Allow: true}).Error(); msg != "" {
		t.Errorf("an allowed decision has no error, got %q", msg)
	}
	msg := Decision{Reason: ReasonRateLimitCooldown, Detail: "quota wall", RetryAfter: 90 * time.Second}.Error()
	for _, want := range []string{"rate_limit_cooldown", "quota wall", "1m30s", "force"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}
