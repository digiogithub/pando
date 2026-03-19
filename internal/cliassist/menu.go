package cliassist

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// MenuAction represents the user's choice from the menu.
type MenuAction int

const (
	ActionExecute    MenuAction = iota // Run the command
	ActionEditPrompt                   // Re-enter the request text and re-fetch
	ActionEditCommand                  // Edit the command inline, then re-show menu
	ActionQuit                         // Exit without executing
)

// ShowMenu displays the generated command in a Unicode box and waits for a keypress.
// Returns the chosen action, the (possibly edited) command, and a new prompt if applicable.
func ShowMenu(command string) (action MenuAction, newCommand string, newPrompt string) {
	const width = 62
	border := strings.Repeat("─", width)

	fmt.Fprintf(os.Stdout, "\n┌%s┐\n", border)
	fmt.Fprintf(os.Stdout, "│ %-*s │\n", width-2, "Generated command:")
	fmt.Fprintf(os.Stdout, "├%s┤\n", border)
	for _, line := range strings.Split(command, "\n") {
		fmt.Fprintf(os.Stdout, "│ %-*s │\n", width-2, truncateLine(line, width-2))
	}
	fmt.Fprintf(os.Stdout, "└%s┘\n", border)
	fmt.Fprintf(os.Stdout, "\n[e] Execute  [p] Edit prompt  [c] Edit command  [q] Quit\n> ")

	// Try raw mode for single-keypress
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return menuFallback(command)
	}

	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
	term.Restore(int(os.Stdin.Fd()), oldState)
	fmt.Fprintln(os.Stdout)

	switch buf[0] {
	case 'e', 'E':
		return ActionExecute, command, ""
	case 'p', 'P':
		fmt.Fprintf(os.Stdout, "New request: ")
		newPrompt = readLine()
		return ActionEditPrompt, command, newPrompt
	case 'c', 'C':
		fmt.Fprintf(os.Stdout, "Edit command:\n%s\n> ", command)
		newCommand = readLineWithDefault(command)
		if newCommand == "" {
			newCommand = command
		}
		return ActionEditCommand, newCommand, ""
	default: // 'q', ESC, Ctrl+C
		return ActionQuit, "", ""
	}
}

// menuFallback is used when raw terminal mode is unavailable (e.g. piped stdin).
func menuFallback(command string) (MenuAction, string, string) {
	fmt.Fprintf(os.Stdout, "Choose [e/p/c/q]: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		switch strings.TrimSpace(strings.ToLower(scanner.Text())) {
		case "e":
			return ActionExecute, command, ""
		case "p":
			fmt.Fprintf(os.Stdout, "New request: ")
			if scanner.Scan() {
				return ActionEditPrompt, command, scanner.Text()
			}
		case "c":
			fmt.Fprintf(os.Stdout, "New command: ")
			if scanner.Scan() {
				return ActionEditCommand, scanner.Text(), ""
			}
		}
	}
	return ActionQuit, "", ""
}

// readLine reads a line from stdin (normal, cooked mode).
func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

// readLineWithDefault shows the default value and reads a new line.
// If the user enters nothing, returns the default.
func readLineWithDefault(defaultVal string) string {
	fmt.Fprintf(os.Stdout, "[current: %s]\n> ", truncateLine(defaultVal, 60))
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		if line := scanner.Text(); line != "" {
			return line
		}
	}
	return defaultVal
}

// truncateLine truncates s to max runes, adding "…" if needed.
func truncateLine(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
