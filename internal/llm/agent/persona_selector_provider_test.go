package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/provider"
)

func TestPersonaSelectorProviderRetry(t *testing.T) {
	old := autoClock
	t.Cleanup(func() { autoClock = old })
	now := time.Unix(1000, 0)
	autoClock = func() time.Time { return now }

	ps := NewLazyPersonaSelector()
	calls := 0

	// Failure is returned, then cached inside the retry interval.
	if _, err := ps.cachedProvider("k", func() (provider.Provider, error) { calls++; return nil, errors.New("boom") }); err == nil {
		t.Fatal("want error")
	}
	now = now.Add(personaProviderRetry - time.Second)
	if _, err := ps.cachedProvider("k", func() (provider.Provider, error) { calls++; return nil, errors.New("boom") }); err == nil {
		t.Fatal("want cached error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry inside interval)", calls)
	}

	// A key change rebuilds immediately.
	if _, err := ps.cachedProvider("k2", func() (provider.Provider, error) { calls++; return nil, errors.New("boom") }); err == nil {
		t.Fatal("want error")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 after key change", calls)
	}

	// After the interval the build is retried and a success is cached for good.
	now = now.Add(personaProviderRetry)
	good := &switchStubProvider{}
	p, err := ps.cachedProvider("k2", func() (provider.Provider, error) { calls++; return good, nil })
	if err != nil || p != provider.Provider(good) || calls != 3 {
		t.Fatalf("retry: p=%v err=%v calls=%d", p, err, calls)
	}
	now = now.Add(time.Hour)
	p, err = ps.cachedProvider("k2", func() (provider.Provider, error) { calls++; return nil, errors.New("x") })
	if err != nil || p != provider.Provider(good) || calls != 3 {
		t.Fatalf("cached success: p=%v err=%v calls=%d", p, err, calls)
	}
}
