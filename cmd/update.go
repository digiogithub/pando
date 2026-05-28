package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/go-github/v30/github"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
	"github.com/spf13/cobra"
)

const selfUpdateRepoSlug = "digiogithub/pando"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the Pando binary from the latest GitHub release",
	Long: `Download the latest compatible Pando release asset from GitHub and replace
this executable in place.

This command only works for released builds with a semantic version such as v0.311.0.`,
	Example: `
  # Update to the latest stable release
  pando update

  # Show whether a newer release is available without changing the binary
  pando update --check
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		checkOnly, _ := cmd.Flags().GetBool("check")
		current, ok := version.Semver()
		if !ok {
			return fmt.Errorf("self-update requires a released semantic version build, current version is %q", version.Version)
		}

		result, err := detectLatestRelease(context.Background())
		if err != nil {
			return err
		}
		if !result.Found {
			return fmt.Errorf("no compatible release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		}
		latest := result.Release

		updater, err := selfupdate.NewUpdater(selfupdate.Config{})
		if err != nil {
			return fmt.Errorf("configure self-update: %w", err)
		}

		currentDisplay := version.Canonical()
		if latest.Version.LTE(current) {
			fmt.Printf("Pando is already up to date (%s)\n", currentDisplay)
			return nil
		}

		fmt.Printf("New version available: %s -> v%s\n", currentDisplay, latest.Version)
		if checkOnly {
			fmt.Printf("Release: %s\n", latest.URL)
			return nil
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}

		if err := updater.UpdateTo(latest, exe); err != nil {
			return fmt.Errorf("apply self-update: %w", err)
		}

		fmt.Printf("Updated Pando to v%s\n", latest.Version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().Bool("check", false, "Only check whether a newer compatible release exists")
}

func startBackgroundUpdateCheck(ctx context.Context) {
	current, ok := version.Semver()
	if !ok {
		return
	}

	go func() {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		result, err := detectLatestRelease(checkCtx)
		if err != nil {
			logging.Debug("startup update check failed", "error", err)
			return
		}
		if !result.Found || result.Release.Version.LTE(current) {
			return
		}

		fmt.Fprintf(os.Stderr, "\nUpdate available: %s -> v%s (run: pando update)\n", version.Canonical(), result.Release.Version)
	}()
}

func detectLatestRelease(ctx context.Context) (*updateCheckResult, error) {
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
		return &updateCheckResult{Release: result.release, Found: result.found}, nil
	}
}

func detectLatestReleaseManual() (*selfupdate.Release, bool, error) {
	client := github.NewClient(nil)
	repo := strings.Split(selfUpdateRepoSlug, "/")
	if len(repo) != 2 {
		return nil, false, fmt.Errorf("invalid repository slug %q", selfUpdateRepoSlug)
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

type updateCheckResult struct {
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
