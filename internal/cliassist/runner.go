package cliassist

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// RunCommand executes the given command in the detected shell.
// It connects stdin/stdout/stderr directly to the terminal so interactive
// commands work. Returns the exit code of the process.
func RunCommand(info SysInfo, command string) int {
	var shellArgs []string

	switch runtime.GOOS {
	case "windows":
		if info.ShellName == "powershell" {
			shellArgs = []string{"powershell.exe", "-NoProfile", "-Command", command}
		} else {
			shellArgs = []string{"cmd.exe", "/C", command}
		}
	default:
		shellArgs = []string{info.ShellPath, "-c", command}
	}

	cmd := exec.Command(shellArgs[0], shellArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Echo the command before execution so the user sees what is running
	fmt.Fprintf(os.Stderr, "\n\033[2m$ %s\033[0m\n\n", command)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "error running command: %v\n", err)
		return 1
	}
	return 0
}
