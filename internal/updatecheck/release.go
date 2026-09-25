// Package updatecheck finds the newest published Pando release on GitHub for
// the running platform. It is shared by the `pando update` command and the
// HTTP API that tells the WebUI whether an update is available.
package updatecheck

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/blang/semver"
	"github.com/google/go-github/v30/github"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
)

// RepoSlug is the GitHub repository releases are published to.
const RepoSlug = "digiogithub/pando"

// DetectLatest returns the newest non-draft GitHub release that ships an asset
// for the running platform. The lookup is abandoned when ctx is done.
func DetectLatest(ctx context.Context) (*Result, error) {
	type response struct {
		release *selfupdate.Release
		found   bool
		err     error
	}

	resultCh := make(chan response, 1)
	go func() {
		release, found, err := detectLatestReleaseManual()
		resultCh <- response{release: release, found: found, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("detect latest release: %w", result.err)
		}
		return &Result{Release: result.release, Found: result.found}, nil
	}
}

func detectLatestReleaseManual() (*selfupdate.Release, bool, error) {
	client := github.NewClient(nil)
	repo := strings.Split(RepoSlug, "/")
	if len(repo) != 2 {
		return nil, false, fmt.Errorf("invalid repository slug %q", RepoSlug)
	}

	releases, _, err := client.Repositories.ListReleases(context.Background(), repo[0], repo[1], nil)
	if err != nil {
		return nil, false, err
	}

	return selectReleaseForTargets(releases, repo[0], repo[1], releaseArchAliases(runtime.GOOS, runtime.GOARCH))
}

func selectReleaseForTargets(releases []*github.RepositoryRelease, repoOwner, repoName string, targets []releaseTarget) (*selfupdate.Release, bool, error) {
	for _, rel := range releases {
		if rel.GetDraft() {
			continue
		}
		parsed, ok := parseReleaseVersion(rel.GetTagName())
		if !ok {
			continue
		}
		for _, target := range targets {
			assetName := fmt.Sprintf("pando-%s-%s.zip", target.OS, target.Arch)
			for _, asset := range rel.Assets {
				if asset.GetName() != assetName {
					continue
				}
				publishedAt := rel.GetPublishedAt().Time
				return &selfupdate.Release{
					Version:       parsed,
					AssetURL:      asset.GetBrowserDownloadURL(),
					AssetByteSize: asset.GetSize(),
					AssetID:       asset.GetID(),
					URL:           rel.GetHTMLURL(),
					ReleaseNotes:  rel.GetBody(),
					Name:          rel.GetName(),
					PublishedAt:   &publishedAt,
					RepoOwner:     repoOwner,
					RepoName:      repoName,
				}, true, nil
			}
		}
	}

	return nil, false, nil
}

func parseReleaseVersion(tag string) (semver.Version, bool) {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return semver.Version{}, false
	}
	if strings.HasPrefix(trimmed, "v") {
		trimmed = trimmed[1:]
	}
	parsed, err := semver.ParseTolerant(trimmed)
	if err != nil {
		return semver.Version{}, false
	}
	return parsed, true
}

// Result is the outcome of a release lookup. Found is false when no release
// carries an asset for the running platform.
type Result struct {
	Release *selfupdate.Release
	Found   bool
}

func updateReleaseFilters() []string {
	targets := releaseArchAliases(runtime.GOOS, runtime.GOARCH)
	filters := make([]string, 0, len(targets))
	for _, target := range targets {
		filters = append(filters, fmt.Sprintf(`^pando[-_]%s[-_]%s\.zip$`, target.OS, target.Arch))
	}
	return filters
}

func releaseAssetPattern() string {
	target := primaryReleaseTarget(runtime.GOOS, runtime.GOARCH)
	return fmt.Sprintf(`^pando[-_]%s[-_]%s\.zip$`, target.OS, target.Arch)
}

type releaseTarget struct {
	OS   string
	Arch string
}

func primaryReleaseTarget(goos, goarch string) releaseTarget {
	aliases := releaseArchAliases(goos, goarch)
	return aliases[0]
}

func releaseArchAliases(goos, goarch string) []releaseTarget {
	normalizedOS := strings.ToLower(strings.TrimSpace(goos))
	normalizedArch := strings.ToLower(strings.TrimSpace(goarch))

	targets := []releaseTarget{{OS: normalizedOS, Arch: normalizedArch}}
	switch normalizedArch {
	case "amd64":
		targets = append([]releaseTarget{{OS: normalizedOS, Arch: "x64"}}, targets...)
	case "arm64":
		if normalizedOS == "darwin" {
			targets = append([]releaseTarget{{OS: normalizedOS, Arch: "arm64"}, {OS: normalizedOS, Arch: "aarch64"}}, targets...)
			return dedupeReleaseTargets(targets)
		}
	}

	return dedupeReleaseTargets(targets)
}

func dedupeReleaseTargets(targets []releaseTarget) []releaseTarget {
	seen := make(map[string]struct{}, len(targets))
	result := make([]releaseTarget, 0, len(targets))
	for _, target := range targets {
		key := target.OS + "/" + target.Arch
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result
}

func releaseArch(goarch string) string {
	return primaryReleaseTarget(runtime.GOOS, goarch).Arch
}
