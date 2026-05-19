package desktop

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// binaryName returns the platform-appropriate binary filename.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "pando-desktop.exe"
	}
	return "pando-desktop"
}

func appBundleName() string {
	return "Pando.app"
}

func hasEmbeddedAppBundle() bool {
	if runtime.GOOS != "darwin" {
		return false
	}

	bundleRoot := "bin/" + appBundleName()
	if _, err := fs.Stat(DesktopBundle, bundleRoot); err == nil {
		return true
	}

	entries, err := fs.Glob(DesktopBundle, bundleRoot+"/**")
	return err == nil && len(entries) > 0
}

func extractEmbeddedAppBundle(dstRoot string) (string, error) {
	bundleRoot := filepath.Join(dstRoot, appBundleName())
	entries, err := fs.Glob(DesktopBundle, "bin/"+appBundleName()+"/**")
	if err != nil {
		return "", fmt.Errorf("failed to list embedded macOS app bundle: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("desktop app bundle not embedded: run `make desktop-embed` on macOS")
	}

	for _, entry := range entries {
		relPath := strings.TrimPrefix(entry, "bin/")
		if relPath == "" {
			continue
		}

		info, statErr := fs.Stat(DesktopBundle, entry)
		if statErr != nil {
			return "", fmt.Errorf("failed to stat embedded app bundle entry %q: %w", entry, statErr)
		}

		targetPath := filepath.Join(dstRoot, filepath.FromSlash(relPath))
		if info.IsDir() {
			if mkdirErr := os.MkdirAll(targetPath, 0o755); mkdirErr != nil {
				return "", fmt.Errorf("failed to create app bundle directory %q: %w", targetPath, mkdirErr)
			}
			continue
		}

		if mkdirErr := os.MkdirAll(filepath.Dir(targetPath), 0o755); mkdirErr != nil {
			return "", fmt.Errorf("failed to create app bundle parent directory %q: %w", targetPath, mkdirErr)
		}

		data, readErr := fs.ReadFile(DesktopBundle, entry)
		if readErr != nil {
			return "", fmt.Errorf("failed to read embedded app bundle entry %q: %w", entry, readErr)
		}

		mode := info.Mode()
		if writeErr := os.WriteFile(targetPath, data, mode.Perm()); writeErr != nil {
			return "", fmt.Errorf("failed to write app bundle entry %q: %w", targetPath, writeErr)
		}
	}

	execPath := filepath.Join(bundleRoot, "Contents", "MacOS", binaryName())
	if chmodErr := os.Chmod(execPath, 0o755); chmodErr != nil {
		return "", fmt.Errorf("failed to ensure macOS app executable permissions: %w", chmodErr)
	}

	return bundleRoot, nil
}

// Launch extracts the embedded desktop binary to a temp dir (if needed) and
// executes it with the given pandoURL. It blocks until the desktop window exits.
//
// The embedBin parameter is the raw bytes of the compiled pando-desktop binary
// produced by `make desktop-embed`. If nil or empty, Launch returns an error.
func Launch(embedBin []byte, pandoURL string, simpleMode bool) error {
	if runtime.GOOS == "darwin" && hasEmbeddedAppBundle() {
		return launchAppBundle(pandoURL, simpleMode)
	}

	if len(embedBin) == 0 {
		return fmt.Errorf("desktop binary not embedded: run `make desktop-embed` to build and embed it")
	}

	tmpDir, err := os.MkdirTemp("", "pando-desktop-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, binaryName())
	if err := os.WriteFile(binPath, embedBin, 0o755); err != nil {
		return fmt.Errorf("failed to write desktop binary: %w", err)
	}

	args := []string{"--url", pandoURL}
	if simpleMode {
		args = append(args, "--simple")
	}

	cmd := exec.Command(binPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 0 {
				return nil
			}
		}
		return fmt.Errorf("desktop process exited: %w", err)
	}
	return nil
}

func launchAppBundle(pandoURL string, simpleMode bool) error {
	tmpDir, err := os.MkdirTemp("", "pando-desktop-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	bundlePath, err := extractEmbeddedAppBundle(tmpDir)
	if err != nil {
		return err
	}

	binPath := filepath.Join(bundlePath, "Contents", "MacOS", binaryName())
	args := []string{"--url", pandoURL}
	if simpleMode {
		args = append(args, "--simple")
	}

	cmd := exec.Command(binPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
			return nil
		}
		return fmt.Errorf("desktop app process exited: %w", err)
	}

	return nil
}
