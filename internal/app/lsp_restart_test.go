package app

import (
	"testing"
	"time"
)

func TestRestartLimiter(t *testing.T) {
	l := newRestartLimiter(2, time.Minute)
	now := time.Now()
	if !l.allow("a", now) || !l.allow("a", now.Add(time.Second)) {
		t.Fatal("first two restarts should be allowed")
	}
	if l.allow("a", now.Add(2*time.Second)) {
		t.Fatal("third restart within window should be denied")
	}
	if !l.allow("b", now) {
		t.Fatal("other clients are tracked independently")
	}
	if !l.allow("a", now.Add(2*time.Minute)) {
		t.Fatal("restarts outside the window should be allowed again")
	}
}
