package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseLoaderErrors(t *testing.T) {
	stderr := "/tmp/pando-desktop-1/pando-desktop: error while loading shared libraries: libwebkit2gtk-4.1.so.0: cannot open shared object file: No such file or directory\n" +
		"/tmp/pando-desktop-1/pando-desktop: error while loading shared libraries: libgtk-3.so.0: cannot open shared object file: No such file or directory\n" +
		"/tmp/pando-desktop-1/pando-desktop: error while loading shared libraries: libwebkit2gtk-4.1.so.0: cannot open shared object file: No such file or directory\n"
	libs, glibc := parseLoaderErrors(stderr)
	want := []string{"libgtk-3.so.0", "libwebkit2gtk-4.1.so.0"}
	if !reflect.DeepEqual(libs, want) {
		t.Fatalf("libs = %v, want %v", libs, want)
	}
	if glibc != "" {
		t.Fatalf("glibc = %q, want empty", glibc)
	}

	_, glibc = parseLoaderErrors("pando-desktop: /lib/x86_64-linux-gnu/libc.so.6: version `GLIBC_2.34' not found (required by pando-desktop)")
	if glibc != "GLIBC_2.34" {
		t.Fatalf("glibc = %q, want GLIBC_2.34", glibc)
	}

	if libs, glibc := parseLoaderErrors("Gtk-WARNING: cannot open display"); libs != nil || glibc != "" {
		t.Fatalf("unexpected match: %v %q", libs, glibc)
	}
}

func TestInstallCommand(t *testing.T) {
	cases := []struct {
		release string
		want    string
	}{
		{"ID=ubuntu\nVERSION_ID=\"22.04\"", "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"},
		{"ID=ubuntu\nVERSION_ID=\"24.04\"", "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64"},
		{"ID=ubuntu\nVERSION_ID=\"26.04\"", "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64"},
		{"ID=pop\nID_LIKE=\"ubuntu debian\"\nVERSION_ID=\"24.04\"", "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64"},
		{"ID=debian\nVERSION_ID=\"12\"", "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"},
		{"ID=fedora\nVERSION_ID=40", "sudo dnf install webkit2gtk4.1 gtk3"},
		{"ID=arch", "sudo pacman -S webkit2gtk-4.1 gtk3"},
		{"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"", "sudo zypper install libwebkit2gtk-4_1-0 gtk3"},
		{"ID=unknownos", ""},
	}
	for _, tc := range cases {
		if got := installCommand(parseOSRelease(tc.release)); got != tc.want {
			t.Errorf("installCommand(%q) = %q, want %q", tc.release, got, tc.want)
		}
	}
}

func TestMissingLibrariesHelpTooOld(t *testing.T) {
	r := parseOSRelease("ID=ubuntu\nVERSION_ID=\"20.04\"\nPRETTY_NAME=\"Ubuntu 20.04.6 LTS\"")
	help := missingLibrariesHelp(r, "")
	if !strings.Contains(help, "Ubuntu 20.04.6 LTS is too old") || !strings.Contains(help, "pando app") {
		t.Fatalf("unexpected help:\n%s", help)
	}
	help = missingLibrariesHelp(parseOSRelease("ID=ubuntu\nVERSION_ID=\"22.04\""), "GLIBC_2.34")
	if !strings.Contains(help, "too old") {
		t.Fatalf("glibc failure should report too old system:\n%s", help)
	}
}

func TestDiagnoseLaunchFailure(t *testing.T) {
	base := errors.New("exit status 127")
	err := diagnoseLaunchFailure("x: error while loading shared libraries: libsoup-3.0.so.0: cannot open shared object file: No such file or directory", base)
	var libErr *MissingLibrariesError
	if !errors.As(err, &libErr) {
		t.Fatalf("expected MissingLibrariesError, got %v", err)
	}
	if !errors.Is(err, base) {
		t.Fatal("MissingLibrariesError must unwrap to the exec error")
	}
	if !strings.Contains(err.Error(), "libsoup-3.0.so.0") || !strings.Contains(err.Error(), "WebKitGTK 4.1") {
		t.Fatalf("unexpected message:\n%s", err.Error())
	}
	if diagnoseLaunchFailure("some unrelated crash", base) != nil {
		t.Fatal("unrelated stderr must not be diagnosed")
	}
}

func TestBoundedBuffer(t *testing.T) {
	var b boundedBuffer
	chunk := strings.Repeat("a", stderrTailSize-10)
	n, _ := b.Write([]byte(chunk))
	if n != len(chunk) {
		t.Fatalf("short write %d", n)
	}
	n, _ = b.Write([]byte(strings.Repeat("b", 100)))
	if n != 100 || len(b.String()) != stderrTailSize {
		t.Fatalf("buffer not bounded: n=%d len=%d", n, len(b.String()))
	}
}

func TestRunDesktopReportsMissingLibraries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("loader diagnosis is Linux only")
	}
	script := filepath.Join(t.TempDir(), "pando-desktop")
	body := "#!/bin/sh\n" +
		"echo \"$0: error while loading shared libraries: libwebkit2gtk-4.1.so.0: cannot open shared object file: No such file or directory\" >&2\n" +
		"exit 127\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runDesktop(script, "http://localhost:1", false)
	var libErr *MissingLibrariesError
	if !errors.As(err, &libErr) {
		t.Fatalf("expected MissingLibrariesError, got %v", err)
	}
	if !reflect.DeepEqual(libErr.Libraries, []string{"libwebkit2gtk-4.1.so.0"}) {
		t.Fatalf("libraries = %v", libErr.Libraries)
	}
}
