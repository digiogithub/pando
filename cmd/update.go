package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
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

		updater, err := selfupdate.NewUpdater(selfupdate.Config{
			Filters: []string{releaseAssetPattern()},
		})
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
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Filters: []string{releaseAssetPattern()},
	})
	if err != nil {
		return nil, fmt.Errorf("configure self-update: %w", err)
	}

	type response struct {
		release *selfupdate.Release
		found   bool
		err     error
	}

	resultCh := make(chan response, 1)
	go func() {
		release, found, err := updater.DetectLatest(selfUpdateRepoSlug)
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

type updateCheckResult struct {
	Release *selfupdate.Release
	Found   bool
}

func releaseAssetPattern() string {
	osName := runtime.GOOS
	archName := releaseArch(runtime.GOARCH)
	return fmt.Sprintf(`^pando[-_]%s[-_]%s\.zip$`, osName, archName)
}

func releaseArch(goarch string) string {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "amd64":
		return "x64"
	default:
		return goarch
	}
}
