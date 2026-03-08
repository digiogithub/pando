package auth

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

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
