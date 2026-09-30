package provider

import "testing"

func TestWithMaxRetriesOverridesTheDefaultBudget(t *testing.T) {
	var opts providerClientOptions
	if got := opts.retryLimit(); got != maxRetries {
		t.Fatalf("default retry limit = %d, want %d", got, maxRetries)
	}
	WithMaxRetries(2)(&opts)
	if got := opts.retryLimit(); got != 2 {
		t.Fatalf("retry limit = %d, want 2", got)
	}
	WithMaxRetries(0)(&opts)
	if got := opts.retryLimit(); got != maxRetries {
		t.Fatalf("a non-positive budget must keep the default, got %d", got)
	}
}
