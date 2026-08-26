package auth

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func CanOpenBrowser() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("open")
		return err == nil
	case "windows":
		_, err := exec.LookPath("rundll32")
		return err == nil
	default:
		if strings.TrimSpace(os.Getenv("DISPLAY")) == "" && strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) == "" {
			return false
		}
		_, err := exec.LookPath("xdg-open")
		return err == nil
	}
}

func OpenBrowser(url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("browser URL cannot be empty")
	}

	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{url}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		command = "xdg-open"
		args = []string{url}
	}

	return exec.Command(command, args...).Start()
}
