package desktop

import (
	"fmt"
	"os"
	"strings"
)

// gtkInitFailure is the panic message Wails raises when gtk_init_check fails,
// which in practice means GTK could not open any display.
const gtkInitFailure = "failed to init GTK"

// NoDisplayError reports that the desktop wrapper cannot open a window because
// no graphical display is reachable (no X11/Wayland session, or WSL without
// WSLg). Its message carries guidance for the detected environment.
type NoDisplayError struct {
	WSL  bool
	Help string
	Err  error
}

func (e *NoDisplayError) Error() string {
	return "desktop wrapper could not open a graphical display\n\n" + e.Help
}

func (e *NoDisplayError) Unwrap() error { return e.Err }

// isWSLProcVersion reports whether a /proc/version string belongs to a WSL
// kernel, whose build string names Microsoft (WSL1 and WSL2 alike).
func isWSLProcVersion(procVersion string) bool {
	return strings.Contains(strings.ToLower(procVersion), "microsoft")
}

// isWSL reports whether this process runs under Windows Subsystem for Linux.
// It is kept local rather than reusing internal/sandbox so the desktop wrapper,
// which imports this package, does not pull in the config stack.
func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return isWSLProcVersion(string(data))
}

// noDisplayHelp renders guidance for a session without a usable display.
func noDisplayHelp(wsl bool, getenv func(string) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GTK could not connect to a display (DISPLAY=%q, WAYLAND_DISPLAY=%q).\n",
		getenv("DISPLAY"), getenv("WAYLAND_DISPLAY"))
	if wsl {
		b.WriteString("Under WSL the desktop app needs WSLg (WSL 2 on Windows 11, or Windows 10 build 19044+):\n")
		b.WriteString("    - From Windows, run `wsl --update` and then `wsl --shutdown`, and reopen the distribution.\n")
		b.WriteString("    - Make sure the distribution uses WSL 2 (`wsl -l -v`) and that `guiApplications=false`\n")
		b.WriteString("      is not set in %UserProfile%\\.wslconfig.\n")
		b.WriteString("    - Check that /mnt/wslg exists and that DISPLAY is set (WSLg uses DISPLAY=:0);\n")
		b.WriteString("      `sudo`, ssh and tmux sessions may drop it.\n")
		b.WriteString("    - If the X11 socket is broken, try: GDK_BACKEND=wayland pando desktop\n")
	} else {
		b.WriteString("Run `pando desktop` from inside a graphical session (X11 or Wayland). Over ssh,\n")
		b.WriteString("`sudo` or tmux the DISPLAY/WAYLAND_DISPLAY variables may be missing.\n")
	}
	b.WriteString("Alternatively, use the browser UI: `pando app`.\n")
	return b.String()
}

func newNoDisplayError(getenv func(string) string, err error) *NoDisplayError {
	wsl := isWSL()
	return &NoDisplayError{WSL: wsl, Help: noDisplayHelp(wsl, getenv), Err: err}
}

// displayEnv inspects the display variables before the wrapper is started. It
// returns extra environment entries for the wrapper, and whether no display is
// available at all.
//
// Wails forces GDK_BACKEND=x11 when XDG_SESSION_TYPE is unset (common under
// WSL), which makes GTK fail on a Wayland-only session; selecting the Wayland
// backend explicitly avoids that.
func displayEnv(getenv func(string) string) (extra []string, noDisplay bool) {
	if strings.TrimSpace(getenv("GDK_BACKEND")) != "" {
		// An explicit backend (including broadway) is the user's choice.
		return nil, false
	}
	hasX11 := strings.TrimSpace(getenv("DISPLAY")) != ""
	hasWayland := strings.TrimSpace(getenv("WAYLAND_DISPLAY")) != ""
	switch {
	case !hasX11 && !hasWayland:
		return nil, true
	case !hasX11:
		return []string{"GDK_BACKEND=wayland"}, false
	}
	return nil, false
}
