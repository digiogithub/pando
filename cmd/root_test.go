package cmd

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/google/go-github/v30/github"
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

func TestParseReleaseVersion(t *testing.T) {
	tests := []struct {
		input  string
		want   string
		wantOK bool
	}{
		{input: "v0.311.0", want: "0.311.0", wantOK: true},
		{input: "0.311.0", want: "0.311.0", wantOK: true},
		{input: "", wantOK: false},
		{input: "not-a-version", wantOK: false},
	}

	for _, tt := range tests {
		got, ok := parseReleaseVersion(tt.input)
		if ok != tt.wantOK {
			t.Fatalf("parseReleaseVersion(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
		}
		if !tt.wantOK {
			continue
		}
		if got.String() != tt.want {
			t.Fatalf("parseReleaseVersion(%q) = %q, want %q", tt.input, got.String(), tt.want)
		}
	}
}

func TestSelectReleaseForTargetsPrefersLinuxX64Asset(t *testing.T) {
	publishedAt := github.Timestamp{Time: time.Date(2026, time.May, 19, 21, 20, 8, 0, time.UTC)}
	releases := []*github.RepositoryRelease{
		{
			TagName:     github.String("v0.311.0"),
			Name:        github.String("v0.311.0"),
			HTMLURL:     github.String("https://github.com/digiogithub/pando/releases/tag/v0.311.0"),
			Body:        github.String("release notes"),
			PublishedAt: &publishedAt,
			Assets: []*github.ReleaseAsset{
				{
					ID:                 github.Int64(424538873),
					Name:               github.String("pando-linux-x64.zip"),
					BrowserDownloadURL: github.String("https://github.com/digiogithub/pando/releases/download/v0.311.0/pando-linux-x64.zip"),
					Size:               github.Int(22181022),
				},
				{
					ID:                 github.Int64(424538851),
					Name:               github.String("pando-linux-arm64.zip"),
					BrowserDownloadURL: github.String("https://github.com/digiogithub/pando/releases/download/v0.311.0/pando-linux-arm64.zip"),
					Size:               github.Int(20799647),
				},
			},
		},
	}

	release, found, err := selectReleaseForTargets(releases, "digiogithub", "pando", []releaseTarget{{OS: "linux", Arch: "x64"}, {OS: "linux", Arch: "amd64"}})
	if err != nil {
		t.Fatalf("selectReleaseForTargets returned error: %v", err)
	}
	if !found {
		t.Fatal("expected to find matching release asset")
	}
	if release.AssetURL != "https://github.com/digiogithub/pando/releases/download/v0.311.0/pando-linux-x64.zip" {
		t.Fatalf("selected asset url = %q", release.AssetURL)
	}
	if release.Version.String() != "0.311.0" {
		t.Fatalf("selected version = %q", release.Version.String())
	}
}

func TestRunACPServerWithOptions_ConfiguresSecondaryIPCFailoverPath(t *testing.T) {
	source, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}
	body := string(source)

	checks := []string{
		"pandoApp.SetIPCSecondaryContext(",
		"rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)",
		"pandoApp.SetupIPC(acpBus)",
		"rt.Watcher.Start(ctx)",
	}
	for _, needle := range checks {
		if !strings.Contains(body, needle) {
			t.Fatalf("runACPServerWithOptions is missing %q", needle)
		}
	}
}

func TestRunACPServerWithOptions_RequiresLogFileWhenDebugEnabled(t *testing.T) {
	err := runACPServerWithOptions(t.TempDir(), true, "", false)
	if err == nil {
		t.Fatal("expected error when ACP debug is enabled without log file")
	}
	if !strings.Contains(err.Error(), "requires --log-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
