package sqlx

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("database is locked (5) (SQLITE_BUSY)"), true},
		{errors.New("SQLITE_BUSY_SNAPSHOT"), true},
		{errors.New("database table is locked (6) (SQLITE_LOCKED)"), true},
		{errors.New("UNIQUE constraint failed"), false},
	}
	for _, c := range cases {
		if got := DefaultRetryable(c.err); got != c.want {
			t.Errorf("DefaultRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRetryLoopRetriesOnlyRetryable(t *testing.T) {
	busy := errors.New("busy")
	calls := 0
	err := RetryLoop(context.Background(), func(err error) bool { return errors.Is(err, busy) }, func() error {
		calls++
		if calls < 4 {
			return busy
		}
		return nil
	})
	if err != nil || calls != 4 {
		t.Fatalf("got err=%v calls=%d, want nil after 4 calls", err, calls)
	}

	calls = 0
	fatal := errors.New("fatal")
	err = RetryLoop(context.Background(), nil, func() error { calls++; return fatal })
	if !errors.Is(err, fatal) || calls != 1 {
		t.Fatalf("non-retryable error: got err=%v calls=%d, want fatal after 1 call", err, calls)
	}
}
