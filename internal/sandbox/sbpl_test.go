package sandbox

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var updateSBPLGolden = flag.Bool("update-sbpl", false, "rewrite testdata/sbpl/*.golden")

// sbplTestPolicy is a hand-built policy with fake paths (the generator never
// touches the filesystem), shaped like a resolved darwin policy.
func sbplTestPolicy(mode Mode, network Network) Policy {
	ws := "/Users/dev/project"
	p := Policy{
		Mode:      mode,
		Network:   network,
		Workspace: ws,
		ProtectedPaths: []string{
			ws + "/.git/config",
			ws + "/.git/hooks",
			ws + "/.pando",
			ws + "/.pando.toml",
			"/Users/dev/.config/pando",
		},
		DenyPaths: []string{
			"/Users/dev/.ssh",
			"/tmp/secrets",
			ws + "/**/*.pem",
			ws + "/config/[!p]rod-?.env",
		},
		// Only emitted while the network is allowed.
		DenyConnectPorts: []int{20001, 8765},
	}
	temps := []string{"/private/tmp", "/private/var/folders", "/private/var/tmp", "/tmp", "/var/tmp"}
	switch mode {
	case ModeWorkspaceWrite:
		p.WritableRoots = append([]string{"/Users/dev/.npm", "/Users/dev/Library/Caches", ws}, temps...)
	case ModeStrict:
		p.WritableRoots = append([]string{ws}, temps...)
		p.ReadableRoots = []string{"/Library", "/System", "/Users/dev/go", "/bin", "/dev", "/etc",
			"/private", "/usr", ws}
	case ModeReadOnly:
		p.WritableRoots = temps
	}
	return p
}

func TestGenerateSBPLGolden(t *testing.T) {
	cases := []struct {
		name    string
		mode    Mode
		network Network
	}{
		{"workspace-write_allowed", ModeWorkspaceWrite, NetworkAllowed},
		{"workspace-write_restricted", ModeWorkspaceWrite, NetworkRestricted},
		{"read-only_allowed", ModeReadOnly, NetworkAllowed},
		{"read-only_restricted", ModeReadOnly, NetworkRestricted},
		{"strict_allowed", ModeStrict, NetworkAllowed},
		{"strict_restricted", ModeStrict, NetworkRestricted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile, params, err := GenerateSBPL(sbplTestPolicy(tc.mode, tc.network))
			if err != nil {
				t.Fatalf("GenerateSBPL: %v", err)
			}
			got := profile + "\n;; -D parameters\n;; " + strings.Join(params, "\n;; ") + "\n"
			path := filepath.Join("testdata", "sbpl", tc.name+".golden")
			if *updateSBPLGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update-sbpl to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("profile differs from %s (run go test -run TestGenerateSBPLGolden -update-sbpl):\n%s", path, got)
			}
			checkSBPLStructure(t, profile, params, tc.mode, tc.network)
		})
	}
}

// checkSBPLStructure asserts the properties the golden files encode, so a
// careless -update-sbpl cannot silently weaken the profile.
func checkSBPLStructure(t *testing.T, profile string, params []string, mode Mode, network Network) {
	t.Helper()
	if !strings.HasPrefix(profile, ";;") || !strings.Contains(profile, "(version 1)\n(deny default)") {
		t.Error("profile must start with (version 1) (deny default)")
	}
	if depth := sbplParenDepth(profile); depth != 0 {
		t.Errorf("unbalanced parentheses (depth %d)", depth)
	}
	allowWrite := strings.Index(profile, "(allow file-write*")
	protect := strings.Index(profile, "(deny file-write*")
	deny := strings.Index(profile, "(deny file-read*")
	if allowWrite < 0 || protect < allowWrite || deny < protect {
		t.Errorf("rule order must be write allows < protected denies < deny paths: %d %d %d", allowWrite, protect, deny)
	}
	if got := strings.Contains(profile, "(allow file-read*)\n"); got != (mode != ModeStrict) {
		t.Errorf("unrestricted reads = %v for mode %s", got, mode)
	}
	restricted := strings.Contains(profile, `(deny network-outbound (remote ip "*:*"))`)
	if restricted != (network == NetworkRestricted) {
		t.Errorf("network restricted = %v, want %v", restricted, network == NetworkRestricted)
	}
	// Guarded ports: denied after the network allow, only when the network
	// is otherwise open.
	portDeny := strings.Index(profile, `(remote tcp "*:8765")`)
	if (portDeny >= 0) == (network == NetworkRestricted) {
		t.Errorf("guarded port deny present = %v with network %s", portDeny >= 0, network)
	}
	if portDeny >= 0 && portDeny < strings.Index(profile, "(allow network*)") {
		t.Error("guarded port deny must follow the network allow")
	}
	// Every param is referenced, every reference is defined, and no path
	// leaks into the profile text.
	for _, kv := range params {
		key, val, _ := strings.Cut(kv, "=")
		if !strings.Contains(profile, `(param "`+key+`")`) {
			t.Errorf("param %s not referenced", key)
		}
		if strings.Contains(profile, `"`+val+`"`) {
			t.Errorf("path %s interpolated into the profile", val)
		}
	}
	for _, m := range regexp.MustCompile(`\(param "([A-Z]+_\d+)"\)`).FindAllStringSubmatch(profile, -1) {
		if !slices.ContainsFunc(params, func(kv string) bool { return strings.HasPrefix(kv, m[1]+"=") }) {
			t.Errorf("param %s referenced but not defined", m[1])
		}
	}
}

func sbplParenDepth(s string) int {
	depth := 0
	inString := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ';' && !inString:
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '"':
			inString = !inString
		case c == '(' && !inString:
			depth++
		case c == ')' && !inString:
			depth--
		}
	}
	return depth
}

func paramValues(params []string, prefix string) []string {
	var out []string
	for _, kv := range params {
		if key, val, _ := strings.Cut(kv, "="); strings.HasPrefix(key, prefix+"_") {
			out = append(out, val)
		}
	}
	return out
}

func TestGenerateSBPLPrivateAliases(t *testing.T) {
	p := Policy{
		Mode:           ModeWorkspaceWrite,
		Network:        NetworkAllowed,
		WritableRoots:  []string{"/tmp/ws"},
		ProtectedPaths: []string{"/private/tmp/ws/.pando"},
		DenyPaths:      []string{"/var/secret", "/etc/master.passwd"},
	}
	_, params, err := GenerateSBPL(p)
	if err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string][]string{
		"WR": {"/tmp/ws", "/private/tmp/ws"},
		"PR": {"/private/tmp/ws/.pando", "/tmp/ws/.pando"},
		"DN": {"/var/secret", "/private/var/secret", "/etc/master.passwd", "/private/etc/master.passwd"},
		// The protected path's parent inside the writable root, both forms.
		"AN": {"/private/tmp/ws", "/tmp/ws"},
	} {
		if got := paramValues(params, prefix); !slices.Equal(got, want) {
			t.Errorf("%s params = %v, want %v", prefix, got, want)
		}
	}
	// Not an alias: /tmpfoo, /variable.
	for _, p := range []string{"/tmpfoo", "/variable/x", "/Users/tmp"} {
		if alias, ok := togglePrivatePrefix(p); ok {
			t.Errorf("togglePrivatePrefix(%q) = %q, want no alias", p, alias)
		}
	}
}

func TestGenerateSBPLWithFilesystemOptions(t *testing.T) {
	ws := "/Users/dev/link"
	p := Policy{
		Mode:           ModeWorkspaceWrite,
		Network:        NetworkAllowed,
		WritableRoots:  []string{ws},
		ProtectedPaths: []string{ws + "/.git/hooks", ws + "/.pando.toml"},
	}
	opts := SBPLOptions{
		Canonical: func(path string) string {
			return strings.Replace(path, "/Users/dev/link", "/Volumes/data/project", 1)
		},
		// .git does not exist yet: it must stay creatable (git init).
		Exists: func(path string) bool { return !strings.HasSuffix(path, "/.git") },
	}
	_, params, err := GenerateSBPLWith(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paramValues(params, "WR"), []string{ws, "/Volumes/data/project"}; !slices.Equal(got, want) {
		t.Errorf("WR = %v, want %v", got, want)
	}
	if got := paramValues(params, "PR"); !slices.Contains(got, "/Volumes/data/project/.pando.toml") {
		t.Errorf("PR lacks the canonical form: %v", got)
	}
	if got, want := paramValues(params, "AN"), []string{ws, "/Volumes/data/project"}; !slices.Equal(got, want) {
		t.Errorf("AN = %v, want %v (missing .git must not be guarded)", got, want)
	}

	// With .git present it is guarded too.
	opts.Exists = nil
	_, params, err = GenerateSBPLWith(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := paramValues(params, "AN"); !slices.Contains(got, ws+"/.git") {
		t.Errorf("AN = %v, want %s/.git", got, ws)
	}
}

func TestGenerateSBPLRejectsBadPaths(t *testing.T) {
	bad := []string{
		"/tmp/new\nline",
		"/tmp/nul\x00byte",
		"/tmp/tab\there",
		`/tmp/quo"te`,
		"/tmp/esc\x1b[0m",
		"relative/path",
		"",
	}
	for _, path := range bad {
		for _, field := range []string{"writable", "readable", "protected", "deny", "denyglob"} {
			p := Policy{Mode: ModeStrict, Network: NetworkRestricted, WritableRoots: []string{"/ok"}}
			switch field {
			case "writable":
				p.WritableRoots = append(p.WritableRoots, path)
			case "readable":
				p.ReadableRoots = []string{"/usr", path}
			case "protected":
				p.ProtectedPaths = []string{path}
			case "deny":
				p.DenyPaths = []string{path}
			case "denyglob":
				if path == "" {
					continue
				}
				p.DenyPaths = []string{path + "/*.key"}
			}
			if _, _, err := GenerateSBPL(p); !errors.Is(err, ErrSBPLPath) {
				t.Errorf("%s %q: err = %v, want ErrSBPLPath", field, path, err)
			}
		}
	}
	// Globs: backslashes and unterminated classes.
	for _, glob := range []string{`/ws/\*.key`, "/ws/[abc.key"} {
		p := Policy{Mode: ModeWorkspaceWrite, Network: NetworkAllowed, DenyPaths: []string{glob}}
		if _, _, err := GenerateSBPL(p); !errors.Is(err, ErrSBPLPath) {
			t.Errorf("glob %q: err = %v, want ErrSBPLPath", glob, err)
		}
	}
	// Disabled policy.
	if _, _, err := GenerateSBPL(Policy{Mode: ModeOff}); err == nil {
		t.Error("GenerateSBPL(off) succeeded")
	}
}

func TestGlobTailToRegex(t *testing.T) {
	cases := []struct {
		glob    string
		match   []string
		noMatch []string
	}{
		{"*.pem", []string{"a.pem", ".pem"}, []string{"a/b.pem", "a.pemx"}},
		{"**/*.pem", []string{"a.pem", "a/b.pem", "a/b/c.pem"}, []string{"a.pe"}},
		{"secret?", []string{"secret1"}, []string{"secret", "secret12", "secret/"}},
		{"[!p]rod.env", []string{"xrod.env"}, []string{"prod.env"}},
		{"a+b(c).txt", []string{"a+b(c).txt"}, []string{"aab(c).txt"}},
	}
	for _, tc := range cases {
		body, err := globTailToRegex(tc.glob)
		if err != nil {
			t.Fatalf("%s: %v", tc.glob, err)
		}
		re := regexp.MustCompile("^" + body + "$")
		for _, s := range tc.match {
			if !re.MatchString(s) {
				t.Errorf("%s (%s) should match %q", tc.glob, body, s)
			}
		}
		for _, s := range tc.noMatch {
			if re.MatchString(s) {
				t.Errorf("%s (%s) should not match %q", tc.glob, body, s)
			}
		}
	}

	// The full deny regex covers the match and everything below it, for
	// both /private forms of the root.
	g := &sbplGen{keys: map[string]string{}}
	rxs, err := g.globRegexes("/tmp/ws/*.key")
	if err != nil {
		t.Fatal(err)
	}
	if len(rxs) != 2 {
		t.Fatalf("regexes = %v, want the /tmp and /private/tmp forms", rxs)
	}
	for _, rx := range rxs {
		re := regexp.MustCompile(rx)
		for _, s := range []string{"/tmp/ws/a.key", "/private/tmp/ws/a.key", "/tmp/ws/a.key/x"} {
			if strings.HasPrefix(s, "/private") != strings.Contains(rx, "private") {
				continue
			}
			if !re.MatchString(s) {
				t.Errorf("%s should match %s", rx, s)
			}
		}
		if re.MatchString("/tmp/ws/sub/a.key") || re.MatchString("/tmp/ws/a.keys") {
			t.Errorf("%s matches too much", rx)
		}
	}
}

func TestSandboxExecArgs(t *testing.T) {
	got := SandboxExecArgs("(version 1)", []string{"WR_0=/a=b"}, []string{"/bin/sh", "-c", "echo hi"})
	want := []string{"sandbox-exec", "-p", "(version 1)", "-D", "WR_0=/a=b", "--", "/bin/sh", "-c", "echo hi"}
	if !slices.Equal(got, want) {
		t.Errorf("SandboxExecArgs = %q, want %q", got, want)
	}
}

func TestCanonicalExisting(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	realDir, _ := filepath.EvalSymlinks(real)
	if got := canonicalExisting(filepath.Join(link, "missing", "file")); got != filepath.Join(realDir, "missing", "file") {
		t.Errorf("canonicalExisting = %q, want under %q", got, realDir)
	}
	if got := canonicalExisting(link); got != realDir {
		t.Errorf("canonicalExisting(link) = %q, want %q", got, realDir)
	}
}

func TestGenerateSBPLGuardedPorts(t *testing.T) {
	p := sbplTestPolicy(ModeWorkspaceWrite, NetworkAllowed)
	p.DenyConnectPorts = []int{9767, 0, 70000, -1, 8765, 9767}
	profile, _, err := GenerateSBPL(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "(allow network*)\n(allow system-socket)\n;; Pando's own TCP ports.\n(deny network-outbound\n" +
		"    (remote tcp \"*:8765\")\n    (remote tcp \"localhost:8765\")\n" +
		"    (remote tcp \"*:9767\")\n    (remote tcp \"localhost:9767\")\n)\n"
	if !strings.Contains(profile, want) {
		t.Fatalf("guarded ports not denied as expected:\n%s", profile)
	}
	for _, bad := range []string{`"*:0"`, `"*:70000"`, `"*:-1"`} {
		if strings.Contains(profile, bad) {
			t.Errorf("invalid port %s emitted", bad)
		}
	}

	p.DenyConnectPorts = nil
	if profile, _, _ := GenerateSBPL(p); strings.Contains(profile, "remote tcp") {
		t.Error("no guarded ports, yet a port rule was emitted")
	}
}
