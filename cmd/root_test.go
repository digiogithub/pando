package cmd

import (
	"runtime"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func TestGoalStatusExitCode(t *testing.T) {
	tests := map[string]int{
		"":                        0,
		agent.GoalStatusCompleted: 0,
		agent.GoalStatusBlocked:   1,
		agent.GoalStatusCancelled: 1,
		agent.GoalStatusStalled:   1,
		agent.GoalStatusFailed:    2,
		agent.GoalStatusTimeout:   2,
		"unexpected":              1,
	}

	for status, want := range tests {
		if got := goalStatusExitCode(status); got != want {
			t.Fatalf("goalStatusExitCode(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestReleaseArchAliases(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   []releaseTarget
	}{
		{
			name:   "linux amd64",
			goos:   "linux",
			goarch: "amd64",
			want: []releaseTarget{
				{OS: "linux", Arch: "x64"},
				{OS: "linux", Arch: "amd64"},
			},
		},
		{
			name:   "darwin amd64",
			goos:   "darwin",
			goarch: "amd64",
			want: []releaseTarget{
				{OS: "darwin", Arch: "x64"},
				{OS: "darwin", Arch: "amd64"},
			},
		},
		{
			name:   "darwin arm64",
			goos:   "darwin",
			goarch: "arm64",
			want: []releaseTarget{
				{OS: "darwin", Arch: "arm64"},
				{OS: "darwin", Arch: "aarch64"},
			},
		},
		{
			name:   "linux arm64",
			goos:   "linux",
			goarch: "arm64",
			want: []releaseTarget{
				{OS: "linux", Arch: "arm64"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseArchAliases(tt.goos, tt.goarch)
			if len(got) != len(tt.want) {
				t.Fatalf("releaseArchAliases(%q, %q) len = %d, want %d (%v)", tt.goos, tt.goarch, len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("releaseArchAliases(%q, %q)[%d] = %+v, want %+v", tt.goos, tt.goarch, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReleaseAssetPatternUsesPrimaryTarget(t *testing.T) {
	target := primaryReleaseTarget(runtime.GOOS, runtime.GOARCH)
	want := `^pando[-_]` + target.OS + `[-_]` + target.Arch + `\.zip$`
	if got := releaseAssetPattern(); got != want {
		t.Fatalf("releaseAssetPattern() = %q, want %q", got, want)
	}
}

func TestUpdateReleaseFiltersIncludePrimaryTarget(t *testing.T) {
	filters := updateReleaseFilters()
	target := primaryReleaseTarget(runtime.GOOS, runtime.GOARCH)
	want := `^pando[-_]` + target.OS + `[-_]` + target.Arch + `\.zip$`
	found := false
	for _, filter := range filters {
		if filter == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected primary filter %q in %v", want, filters)
	}
}
