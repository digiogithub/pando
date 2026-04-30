package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// binaryName returns the platform-appropriate binary filename.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "pando-desktop.exe"
	}
	return "pando-desktop"
}

// Launch extracts the embedded desktop binary to a temp dir (if needed) and
// executes it with the given pandoURL. It blocks until the desktop window exits.
//
// The embedBin parameter is the raw bytes of the compiled pando-desktop binary
// produced by `make desktop-embed`. If nil or empty, Launch returns an error.
func Launch(embedBin []byte, pandoURL string, simpleMode bool) error {
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
