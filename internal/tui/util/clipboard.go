package util

import (
	"os"
	"strings"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// CopyToClipboard places text on the clipboard. It makes a best-effort call to
// the OS clipboard helper (xclip/xsel/wl-copy/pbcopy) AND always emits an OSC 52
// escape sequence to the terminal. OSC 52 needs no helper binary and works over
// SSH and on Wayland sessions where those helpers are frequently absent
// (WezTerm, COSMIC Terminal, ...), which is why the OS-helper path alone failed
// silently. Returns false only for empty input.
//
// It is a package variable, not a plain function, so callers that need to
// verify a copy happened (settings actions, tests) can swap in a fake without
// touching a real terminal or the OS clipboard.
var CopyToClipboard = copyToClipboard

func copyToClipboard(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	// Best-effort; commonly errors on headless/Wayland with no helper installed.
	_ = clipboard.WriteAll(text)
	seq := osc52.New(text)
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	}
	_, _ = seq.WriteTo(os.Stdout)
	return true
}
