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
//
// PANDO_NATIVE_CAPTURE_PATHS=/chat,/settings/appearance,... visits the
// routes in turn instead (after the first PANDO_NATIVE_CAPTURE_AT second,
// PANDO_NATIVE_CAPTURE_STEP seconds apart, default 3) and writes
// <dir>/capture-<route as slug>.png for each.
func startCapture(w *mygo.Window, navigate func(path string)) {
	dir := os.Getenv("PANDO_NATIVE_CAPTURE")
	if dir == "" {
		return
	}
	if paths := os.Getenv("PANDO_NATIVE_CAPTURE_PATHS"); paths != "" {
		go capturePaths(w, dir, strings.Split(paths, ","), navigate)
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

func capturePaths(w *mygo.Window, dir string, paths []string, navigate func(string)) {
	first, step := envSeconds("PANDO_NATIVE_CAPTURE_AT", 6), envSeconds("PANDO_NATIVE_CAPTURE_STEP", 3)
	time.Sleep(first)
	_ = os.MkdirAll(dir, 0o755)
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		w.Update(func() { navigate(p) })
		time.Sleep(step)
		png, err := w.CapturePage()
		if err != nil {
			fmt.Fprintln(os.Stderr, "nativeui capture:", p, err)
			continue
		}
		slug := strings.Trim(strings.NewReplacer("/", "-", "?", "-", "=", "-").Replace(p), "-")
		_ = os.WriteFile(filepath.Join(dir, "capture-"+slug+".png"), png, 0o644)
	}
}

// envSeconds reads the first number of a comma separated seconds list.
func envSeconds(name string, def int) time.Duration {
	v := strings.TrimSpace(strings.Split(os.Getenv(name), ",")[0])
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return time.Duration(def) * time.Second
}
