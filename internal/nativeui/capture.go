package nativeui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// startCapture is a development aid for headless verification: with
// PANDO_NATIVE_CAPTURE=<dir> the window writes what it renders to
// <dir>/capture-<n>s.png at the seconds listed in PANDO_NATIVE_CAPTURE_AT
// (comma separated, default "6"). It uses MyGo's own renderer, so it works
// where compositor screenshots are not allowed.
func startCapture(w *mygo.Window) {
	dir := os.Getenv("PANDO_NATIVE_CAPTURE")
	if dir == "" {
		return
	}
	at := os.Getenv("PANDO_NATIVE_CAPTURE_AT")
	if at == "" {
		at = "6"
	}
	for _, f := range strings.Split(at, ",") {
		sec, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || sec < 0 {
			continue
		}
		go func() {
			time.Sleep(time.Duration(sec) * time.Second)
			png, err := w.CapturePage()
			if err != nil {
				fmt.Fprintln(os.Stderr, "nativeui capture:", err)
				return
			}
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("capture-%ds.png", sec)), png, 0o644)
		}()
	}
}
