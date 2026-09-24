package desktop

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The dynamic loader (ld.so) reports unresolved dependencies of the Linux
// wrapper on stderr before the wrapper's own code ever runs, e.g.:
//
//	pando-desktop: error while loading shared libraries: libwebkit2gtk-4.1.so.0: cannot open shared object file: No such file or directory
//	pando-desktop: /lib/x86_64-linux-gnu/libc.so.6: version `GLIBC_2.34' not found (required by pando-desktop)
var (
	missingLibRe   = regexp.MustCompile(`error while loading shared libraries: ([^:\s]+): cannot open shared object file`)
	missingGlibcRe = regexp.MustCompile("version `(GLIBC_[0-9.]+)' not found")
)

// stderrTailSize bounds how much wrapper stderr is kept for diagnosis. Loader
// errors are printed first and are short, so a small window is enough.
const stderrTailSize = 16 * 1024

// MissingLibrariesError reports that the desktop wrapper could not start
// because the system lacks the GTK/WebKitGTK runtime (or has a too old glibc).
// Its message carries distro-specific install instructions.
type MissingLibrariesError struct {
	Libraries []string // missing shared objects, e.g. libwebkit2gtk-4.1.so.0
	Glibc     string   // required glibc symbol version, e.g. GLIBC_2.34
	Help      string
	Err       error
}

func (e *MissingLibrariesError) Error() string {
	var what []string
	if len(e.Libraries) > 0 {
		what = append(what, "missing shared libraries: "+strings.Join(e.Libraries, ", "))
	}
	if e.Glibc != "" {
		what = append(what, "system glibc is older than required "+e.Glibc)
	}
	return fmt.Sprintf("desktop wrapper could not start (%s)\n\n%s", strings.Join(what, "; "), e.Help)
}

func (e *MissingLibrariesError) Unwrap() error { return e.Err }

// boundedBuffer keeps the first stderrTailSize bytes written to it; it is safe
// for the concurrent writes exec.Cmd may issue.
type boundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := stderrTailSize - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// parseLoaderErrors extracts missing shared libraries and a required glibc
// version from dynamic loader output. Libraries are deduplicated and sorted.
func parseLoaderErrors(stderr string) (libs []string, glibc string) {
	seen := map[string]bool{}
	for _, m := range missingLibRe.FindAllStringSubmatch(stderr, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			libs = append(libs, m[1])
		}
	}
	sort.Strings(libs)
	if m := missingGlibcRe.FindStringSubmatch(stderr); m != nil {
		glibc = m[1]
	}
	return libs, glibc
}

// osRelease holds the /etc/os-release fields needed to pick install commands.
type osRelease struct {
	ID        string // ubuntu, debian, fedora, arch, opensuse-leap, ...
	IDLike    string // space separated parents, e.g. "debian" or "ubuntu debian"
	VersionID string // 22.04, 24.04, 12, 40, ...
	Pretty    string
}

func readOSRelease() osRelease {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := os.ReadFile(path)
		if err == nil {
			return parseOSRelease(string(data))
		}
	}
	return osRelease{}
}

func parseOSRelease(data string) osRelease {
	var r osRelease
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"'`)
		switch key {
		case "ID":
			r.ID = strings.ToLower(val)
		case "ID_LIKE":
			r.IDLike = strings.ToLower(val)
		case "VERSION_ID":
			r.VersionID = val
		case "PRETTY_NAME":
			r.Pretty = val
		}
	}
	return r
}

func (r osRelease) is(family string) bool {
	if r.ID == family {
		return true
	}
	for _, like := range strings.Fields(r.IDLike) {
		if like == family {
			return true
		}
	}
	return false
}

// ubuntuAtLeast reports whether an Ubuntu VERSION_ID ("24.04") is >= major.minor.
func ubuntuAtLeast(versionID string, major, minor int) bool {
	var maj, mnr int
	if _, err := fmt.Sscanf(versionID, "%d.%d", &maj, &mnr); err != nil {
		return false
	}
	return maj > major || (maj == major && mnr >= minor)
}

// installCommand returns the package manager command that installs the GTK3 +
// WebKitGTK 4.1 runtime the wrapper links against, or "" when unknown.
func installCommand(r osRelease) string {
	switch {
	case r.is("ubuntu") && r.VersionID != "":
		// Ubuntu 24.04+ renamed the GTK3 runtime to libgtk-3-0t64 (64-bit
		// time_t transition); 22.04 still ships libgtk-3-0.
		if ubuntuAtLeast(r.VersionID, 24, 4) {
			return "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64"
		}
		return "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"
	case r.is("debian") || r.is("ubuntu"):
		return "sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"
	case r.is("fedora") || r.is("rhel"):
		return "sudo dnf install webkit2gtk4.1 gtk3"
	case r.is("arch"):
		return "sudo pacman -S webkit2gtk-4.1 gtk3"
	case r.is("suse") || strings.HasPrefix(r.ID, "opensuse"):
		return "sudo zypper install libwebkit2gtk-4_1-0 gtk3"
	}
	return ""
}

// missingLibrariesHelp renders install guidance for the detected distro.
func missingLibrariesHelp(r osRelease, glibc string) string {
	var b strings.Builder
	b.WriteString("Pando desktop needs the GTK 3 and WebKitGTK 4.1 runtime libraries.\n")

	tooOldUbuntu := r.ID == "ubuntu" && r.VersionID != "" && !ubuntuAtLeast(r.VersionID, 22, 4)
	if glibc != "" || tooOldUbuntu {
		name := r.Pretty
		if name == "" {
			name = "This system"
		}
		fmt.Fprintf(&b, "%s is too old for the desktop app: it requires glibc 2.34+ and WebKitGTK 4.1\n", name)
		b.WriteString("(Ubuntu 22.04 LTS or newer, Debian 12 or newer).\n")
		b.WriteString("Upgrade the distribution, or use the browser UI instead: `pando app`.\n")
		return b.String()
	}

	if cmd := installCommand(r); cmd != "" {
		b.WriteString("Install them with:\n\n    " + cmd + "\n\n")
	} else {
		b.WriteString("Install your distribution's GTK 3 and WebKitGTK 4.1 runtime packages.\n\n")
	}
	b.WriteString("Package names per Ubuntu LTS release:\n")
	b.WriteString("    Ubuntu 22.04:         sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0\n")
	b.WriteString("    Ubuntu 24.04, 26.04:  sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64\n")
	b.WriteString("Alternatively, use the browser UI: `pando app`.\n")
	return b.String()
}

// diagnoseLaunchFailure turns a failed wrapper run into a MissingLibrariesError
// when its stderr shows loader errors; otherwise it returns nil.
func diagnoseLaunchFailure(stderr string, err error) error {
	libs, glibc := parseLoaderErrors(stderr)
	if len(libs) == 0 && glibc == "" {
		return nil
	}
	return &MissingLibrariesError{
		Libraries: libs,
		Glibc:     glibc,
		Help:      missingLibrariesHelp(readOSRelease(), glibc),
		Err:       err,
	}
}
