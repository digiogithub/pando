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

// siblingDesktopBinary returns the path of a pando-desktop wrapper shipped on
// disk next to the running executable, if present.
//
// Packaged macOS builds (the signed & notarized Pando.app produced by
// scripts/build-macos-app) place pando-desktop alongside pando-bin inside
// Contents/MacOS/. Launching that on-disk wrapper directly avoids extracting an
// embedded Mach-O to a temp dir — extraction strips the file from its signed,
// notarized location, so the hardened runtime of the parent process would kill
// the unsigned/quarantine-checked child. Preferring the sibling keeps the
// wrapper running from its validly signed final path.
func siblingDesktopBinary() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	candidate := filepath.Join(filepath.Dir(exe), binaryName())
	if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}

// runDesktop executes the desktop wrapper at binPath and blocks until it exits.
func runDesktop(binPath, pandoURL string, simpleMode bool) error {
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
		return fmt.Errorf("desktop process exited: %w", err)
	}
	return nil
}

func startDesktop(binPath string, args []string) error {
	cmd := exec.Command(binPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start desktop process: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release desktop process: %w", err)
	}
	return nil
}

func resolveLaunchBinary(embedBin []byte) (binPath string, cleanupDir string, err error) {
	if sibling, ok := siblingDesktopBinary(); ok {
		return sibling, "", nil
	}

	if runtime.GOOS == "darwin" && hasUsableEmbeddedAppBundle() {
		tmpDir, err := os.MkdirTemp("", "pando-desktop-*")
		if err != nil {
			return "", "", fmt.Errorf("failed to create temp dir: %w", err)
		}
		bundlePath, err := extractEmbeddedAppBundle(tmpDir)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", err
		}
		binPath, err := macOSBundleExecutablePath(bundlePath)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", err
		}
		return binPath, tmpDir, nil
	}

	if len(embedBin) == 0 {
		if runtime.GOOS == "darwin" && hasEmbeddedAppBundle() {
			return "", "", fmt.Errorf("embedded macOS app bundle is incomplete: re-run `make desktop-embed` on macOS so Pando.app includes Contents/Info.plist and Contents/MacOS/*")
		}
		return "", "", fmt.Errorf("desktop binary not embedded: run `make desktop-embed` to build and embed it")
	}

	tmpDir, err := os.MkdirTemp("", "pando-desktop-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	binPath = filepath.Join(tmpDir, binaryName())
	if err := os.WriteFile(binPath, embedBin, 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("failed to write desktop binary: %w", err)
	}
	return binPath, tmpDir, nil
}

// Launch starts the desktop wrapper and blocks until the desktop window exits.
//
// Resolution order:
//  1. A pando-desktop wrapper shipped on disk next to the running executable
//     (packaged .app/.pkg installs) — launched in place, preserving its
//     signature & notarization.
//  2. On macOS, an embedded Pando.app bundle extracted to a temp dir.
//  3. The raw embedBin bytes (the compiled pando-desktop produced by
//     `make desktop-embed`) extracted to a temp dir.
//
// embedBin may be nil/empty when the wrapper is shipped on disk instead of
// embedded.
func Launch(embedBin []byte, pandoURL string, simpleMode bool) error {
	binPath, cleanupDir, err := resolveLaunchBinary(embedBin)
	if err != nil {
		return err
	}
	if cleanupDir != "" {
		defer os.RemoveAll(cleanupDir)
	}
	return runDesktop(binPath, pandoURL, simpleMode)
}

// LaunchWindow starts a desktop wrapper for url and returns immediately.
//
// When LaunchWindow has to extract an embedded wrapper or app bundle, the temp
// directory is intentionally left behind so the child process keeps its files
// for its whole lifetime; the OS temp cleanup can reclaim it later.
func LaunchWindow(embedBin []byte, url string) error {
	binPath, _, err := resolveLaunchBinary(embedBin)
	if err != nil {
		return err
	}
	return startDesktop(binPath, []string{"--url", url})
}
