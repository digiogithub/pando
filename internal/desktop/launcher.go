package desktop

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

	bundleInfoPath := "bin/" + appBundleName() + "/Contents/Info.plist"
	_, err := fs.Stat(DesktopBundle, bundleInfoPath)
	return err == nil
}

func hasUsableEmbeddedAppBundle() bool {
	if !hasEmbeddedAppBundle() {
		return false
	}

	if _, err := fs.Stat(DesktopBundle, "bin/"+appBundleName()+"/Contents/MacOS"); err == nil {
		return true
	}

	entries, err := fs.Glob(DesktopBundle, "bin/"+appBundleName()+"/Contents/MacOS/*")
	return err == nil && len(entries) > 0
}

func extractEmbeddedAppBundle(dstRoot string) (string, error) {
	bundleRoot := filepath.Join(dstRoot, appBundleName())
	if err := copyEmbeddedDir(DesktopBundle, "bin/"+appBundleName(), bundleRoot); err != nil {
		return "", err
	}

	execPath, err := macOSBundleExecutablePath(bundleRoot)
	if err != nil {
		return "", err
	}
	if chmodErr := os.Chmod(execPath, 0o755); chmodErr != nil {
		return "", fmt.Errorf("failed to ensure macOS app executable permissions: %w", chmodErr)
	}

	return bundleRoot, nil
}

func copyEmbeddedDir(src fs.FS, srcRoot, dstRoot string) error {
	return fs.WalkDir(src, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk embedded app bundle entry %q: %w", path, err)
		}

		relPath, relErr := filepath.Rel(srcRoot, path)
		if relErr != nil {
			return fmt.Errorf("failed to resolve embedded app bundle relative path for %q: %w", path, relErr)
		}
		if relPath == "." {
			return os.MkdirAll(dstRoot, 0o755)
		}

		targetPath := filepath.Join(dstRoot, filepath.FromSlash(relPath))
		if d.IsDir() {
			if mkdirErr := os.MkdirAll(targetPath, 0o755); mkdirErr != nil {
				return fmt.Errorf("failed to create app bundle directory %q: %w", targetPath, mkdirErr)
			}
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("failed to stat embedded app bundle entry %q: %w", path, infoErr)
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(targetPath), 0o755); mkdirErr != nil {
			return fmt.Errorf("failed to create app bundle parent directory %q: %w", targetPath, mkdirErr)
		}

		data, readErr := fs.ReadFile(src, path)
		if readErr != nil {
			return fmt.Errorf("failed to read embedded app bundle entry %q: %w", path, readErr)
		}
		if writeErr := os.WriteFile(targetPath, data, info.Mode().Perm()); writeErr != nil {
			return fmt.Errorf("failed to write app bundle entry %q: %w", targetPath, writeErr)
		}
		return nil
	})
}

func macOSBundleExecutablePath(bundleRoot string) (string, error) {
	infoPlistPath := filepath.Join(bundleRoot, "Contents", "Info.plist")
	data, err := os.ReadFile(infoPlistPath)
	if err != nil {
		return "", fmt.Errorf("failed to read macOS app Info.plist: %w", err)
	}

	match := regexp.MustCompile(`<key>CFBundleExecutable</key>\s*<string>([^<]+)</string>`).FindSubmatch(data)
	if len(match) != 2 {
		return "", fmt.Errorf("failed to determine macOS app executable from Info.plist")
	}

	return filepath.Join(bundleRoot, "Contents", "MacOS", string(match[1])), nil
}

// Launch extracts the embedded desktop binary to a temp dir (if needed) and
// executes it with the given pandoURL. It blocks until the desktop window exits.
//
// The embedBin parameter is the raw bytes of the compiled pando-desktop binary
// produced by `make desktop-embed`. If nil or empty, Launch returns an error.
func Launch(embedBin []byte, pandoURL string, simpleMode bool) error {
	if runtime.GOOS == "darwin" && hasUsableEmbeddedAppBundle() {
		return launchAppBundle(pandoURL, simpleMode)
	}

	if len(embedBin) == 0 {
		if runtime.GOOS == "darwin" && hasEmbeddedAppBundle() {
			return fmt.Errorf("embedded macOS app bundle is incomplete: re-run `make desktop-embed` on macOS so Pando.app includes Contents/Info.plist and Contents/MacOS/*")
		}
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

	binPath, err := macOSBundleExecutablePath(bundlePath)
	if err != nil {
		return err
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
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
			return nil
		}
		return fmt.Errorf("desktop app process exited: %w", err)
	}

	return nil
}
