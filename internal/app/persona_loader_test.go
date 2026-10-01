package app

import (
	"testing"
	"time"
)

func TestPersonaLoadDue(t *testing.T) {
	now := time.Unix(10000, 0)
	cases := []struct {
		name     string
		loadedOK bool
		last     string
		path     string
		failedAt time.Time
		force    bool
		want     bool
	}{
		{"first load", false, "", "", time.Time{}, false, true},
		{"loaded unchanged", true, "/p", "/p", time.Time{}, false, false},
		{"loaded unchanged config event", true, "/p", "/p", time.Time{}, true, false},
		{"path changed", true, "/p", "/q", time.Time{}, false, true},
		{"failed recently from prompt", false, "/p", "/p", now.Add(-5 * time.Second), false, false},
		{"failed recently config event", false, "/p", "/p", now.Add(-5 * time.Second), true, true},
		{"failed long ago from prompt", false, "/p", "/p", now.Add(-personaLoadRetry), false, true},
		{"failed then path changed", false, "/p", "/q", now, false, true},
	}
	for _, tc := range cases {
		if got := personaLoadDue(tc.loadedOK, tc.last, tc.path, tc.failedAt, now, tc.force); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
