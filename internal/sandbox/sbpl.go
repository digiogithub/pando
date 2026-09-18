package sandbox

// SBPL (Seatbelt Profile Language) generation for the macOS backend
// (wrapper_darwin.go). This file has no build tag on purpose: the generator
// is pure, so its golden tests run on every OS in CI.
//
// Design (research C.4, Grok Build xai-grok-sandbox deny/mod.rs):
//   - (deny default), then allows, then denies. Seatbelt applies the LAST
//     matching rule for an operation, so the protected/deny rules are emitted
//     after the broad file-write allows and win inside writable roots.
//   - Every path reaches the profile as a sandbox-exec `-D KEY=VALUE`
//     parameter referenced by (param "KEY"), never interpolated into the
//     profile text, so no escaping bug can change a rule. Paths holding a
//     control character or a double quote are rejected outright.
//   - macOS reaches /tmp, /var and /etc through symlinks into /private, and
//     Seatbelt matches the resolved path. Each path is emitted in every form:
//     as given, canonical (symlinks resolved) and the /private alias of each.
//   - Deny globs (DenyPaths entries with *, ? or [) become anchored Seatbelt
//     regexes, which also cover files created after launch.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// SBPLOptions lets GenerateSBPLWith consult the filesystem. A nil function
// means "no filesystem": GenerateSBPL uses the zero value, which keeps the
// generator pure (golden tests pass fake paths).
type SBPLOptions struct {
	// Canonical returns the symlink-resolved form of an absolute path (for a
	// missing path: its deepest existing ancestor resolved, plus the rest).
	// nil means the identity.
	Canonical func(path string) string
	// Exists reports whether a path exists (without following a final
	// symlink). It only gates the ancestor rename guards: a missing ancestor
	// (e.g. <ws>/.git before `git init`) must stay creatable. nil means every
	// path exists.
	Exists func(path string) bool
}

// OSSBPLOptions returns SBPLOptions backed by the real filesystem, as used by
// the macOS wrapper.
func OSSBPLOptions() SBPLOptions {
	return SBPLOptions{Canonical: canonicalExisting, Exists: pathExists}
}

// ErrSBPLPath marks a path that cannot be expressed in an SBPL profile.
var ErrSBPLPath = errors.New("sandbox: path cannot be used in a Seatbelt profile")

// seatbeltWriteDenyActions are the specific file-write sub-operations denied
// next to file-write* for protected and deny paths. Grok found that a bare
// (deny file-write* ...) does not always win against a broader write allow;
// the specific actions do (deny/mod.rs SEATBELT_WRITE_DENY_ACTIONS).
const seatbeltWriteDenyActions = "file-write* file-write-data file-write-create file-write-unlink " +
	"file-write-mode file-write-owner file-write-flags file-write-times file-write-setugid"

// seatbeltAncestorDenyActions guard the existing parents of a protected or
// denied path inside a writable root: unlink blocks renaming the parent away
// (then editing the moved copy and renaming it back), create blocks renaming
// a prepared directory onto it (deny/mod.rs SEATBELT_ANCESTOR_NODE_DENY_ACTIONS).
const seatbeltAncestorDenyActions = "file-write-unlink file-write-create"

// GenerateSBPL builds the Seatbelt profile for p without touching the
// filesystem: paths are used as given (plus their /private aliases) and every
// ancestor is assumed to exist. It returns the profile text and the
// sandbox-exec parameters as "KEY=VALUE" strings, each to be passed after a
// "-D" flag (see SandboxExecArgs).
//
// A disabled policy is an error: there is nothing to generate.
func GenerateSBPL(p Policy) (profile string, params []string, err error) {
	return GenerateSBPLWith(p, SBPLOptions{})
}

// GenerateSBPLWith is GenerateSBPL consulting the filesystem through opts:
// symlinked roots get their resolved form too and the ancestor rename guards
// only cover parents that exist.
func GenerateSBPLWith(p Policy, opts SBPLOptions) (profile string, params []string, err error) {
	if !p.Enabled() {
		return "", nil, fmt.Errorf("sandbox: cannot generate a Seatbelt profile for mode %q", p.Mode)
	}
	g := &sbplGen{opts: opts, keys: map[string]string{}}

	writable, err := g.expandAll(p.WritableRoots)
	if err != nil {
		return "", nil, err
	}
	var readable []string
	if len(p.ReadableRoots) > 0 {
		if readable, err = g.expandAll(p.ReadableRoots); err != nil {
			return "", nil, err
		}
	}
	protected, err := g.expandAll(p.ProtectedPaths)
	if err != nil {
		return "", nil, err
	}
	var denyExact, denyGlobs []string
	for _, d := range p.DenyPaths {
		if sbplIsGlob(d) {
			denyGlobs = append(denyGlobs, d)
		} else {
			denyExact = append(denyExact, d)
		}
	}
	deny, err := g.expandAll(denyExact)
	if err != nil {
		return "", nil, err
	}
	var denyRegexes []string
	for _, glob := range denyGlobs {
		rx, err := g.globRegexes(glob)
		if err != nil {
			return "", nil, err
		}
		denyRegexes = appendUnique(denyRegexes, rx...)
	}
	// Ancestor guards for protected and exact deny paths (as given and
	// canonical), relative to the writable roots in all their forms.
	var ancestors []string
	for _, list := range [][]string{p.ProtectedPaths, denyExact} {
		for _, path := range list {
			for _, form := range g.forms(path) {
				ancestors = appendUnique(ancestors, g.ancestorsWithin(form, writable)...)
			}
		}
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w(";; Pando sandbox profile (mode %s, network %s). Generated; do not edit.\n", p.Mode, p.Network)
	w("(version 1)\n(deny default)\n\n")

	w(";; Processes, signals and system information.\n")
	w("(allow process-exec)\n(allow process-fork)\n")
	w("(allow process-info* (target same-sandbox))\n")
	w("(allow signal (target same-sandbox))\n")
	w("(allow sysctl-read)\n")
	w("(allow mach-lookup)\n")
	w("(allow ipc-posix-shm*)\n(allow ipc-posix-sem)\n")
	w("(allow user-preference-read)\n")
	w("(allow pseudo-tty)\n\n")

	w(";; Reads.\n")
	if len(readable) == 0 {
		w("(allow file-read*)\n\n")
	} else {
		// Strict: only the readable roots; metadata (stat) everywhere so
		// path traversal and module resolution keep working.
		w("(allow file-read-metadata)\n")
		w("(allow file-read*\n    (literal \"/\")\n")
		for _, r := range readable {
			w("    (subpath (param %q))\n", g.param("RD", r))
		}
		w(")\n\n")
	}

	w(";; Writes: the writable roots and the terminal/null devices.\n")
	w("(allow file-write*\n")
	for _, r := range writable {
		w("    (subpath (param %q))\n", g.param("WR", r))
	}
	w("    (literal \"/dev/null\")\n    (literal \"/dev/zero\")\n    (literal \"/dev/tty\")\n")
	w("    (literal \"/dev/ptmx\")\n    (literal \"/dev/stdout\")\n    (literal \"/dev/stderr\")\n")
	w("    (literal \"/dev/dtracehelper\")\n    (regex #\"^/dev/ttys[0-9]+$\")\n    (subpath \"/dev/fd\")\n")
	w(")\n")
	w("(allow file-read* file-write* file-ioctl\n")
	w("    (literal \"/dev/null\")\n    (literal \"/dev/tty\")\n    (literal \"/dev/ptmx\")\n")
	w("    (regex #\"^/dev/ttys[0-9]+$\")\n    (subpath \"/dev/fd\")\n")
	w(")\n\n")

	w(";; Network.\n")
	if p.RestrictsNetwork() {
		// Local (unix-domain) sockets keep working; every IP address, local
		// or remote, is denied.
		w("(allow network*)\n")
		w("(deny network-outbound (remote ip \"*:*\"))\n")
		w("(deny network-inbound (local ip \"*:*\"))\n")
		w("(deny network-bind (local ip \"*:*\"))\n\n")
	} else {
		w("(allow network*)\n(allow system-socket)\n")
		if ports := cleanPorts(p.DenyConnectPorts); len(ports) > 0 {
			// Pando's own listeners (API, IPC bus, MCP HTTP, ...) stay
			// unreachable: they can turn the sandbox off or run commands
			// outside it. Emitted after the allow (the last match wins);
			// ports are validated integers, never caller text.
			w(";; Pando's own TCP ports.\n")
			w("(deny network-outbound\n")
			for _, port := range ports {
				w("    (remote tcp \"*:%d\")\n    (remote tcp \"localhost:%d\")\n", port, port)
			}
			w(")\n")
		}
		w("\n")
	}

	if len(protected) > 0 {
		w(";; Protected paths stay read-only inside writable roots (emitted after\n")
		w(";; the allows: the last matching rule wins).\n")
		w("(deny %s\n", seatbeltWriteDenyActions)
		for _, r := range protected {
			w("    (subpath (param %q))\n", g.param("PR", r))
		}
		w(")\n\n")
	}
	if len(ancestors) > 0 {
		w(";; Existing parents of protected/denied paths cannot be renamed away or replaced.\n")
		w("(deny %s\n", seatbeltAncestorDenyActions)
		for _, r := range ancestors {
			w("    (literal (param %q))\n", g.param("AN", r))
		}
		w(")\n\n")
	}
	if len(deny) > 0 || len(denyRegexes) > 0 {
		w(";; Denied paths: no read, no write.\n")
		w("(deny file-read* %s\n", seatbeltWriteDenyActions)
		for _, r := range deny {
			w("    (subpath (param %q))\n", g.param("DN", r))
		}
		for _, rx := range denyRegexes {
			w("    (regex #\"%s\")\n", rx)
		}
		w(")\n")
	}

	return b.String(), g.params, nil
}

// SandboxExecArgs returns the argv for /usr/bin/sandbox-exec running argv
// (argv[0] is the program path) under profile with params ("KEY=VALUE"):
// [sandbox-exec -p <profile> -D K=V ... -- argv...]. sandbox-exec parses its
// flags with getopt(3), so "--" ends them and a program whose name starts
// with "-" is still run.
func SandboxExecArgs(profile string, params []string, argv []string) []string {
	out := make([]string, 0, 4+2*len(params)+len(argv))
	out = append(out, "sandbox-exec", "-p", profile)
	for _, kv := range params {
		out = append(out, "-D", kv)
	}
	out = append(out, "--")
	return append(out, argv...)
}

// sbplGen accumulates parameters while a profile is generated.
type sbplGen struct {
	opts   SBPLOptions
	params []string
	// keys maps "<prefix>\x00<path>" to the parameter name already assigned.
	keys map[string]string
	next map[string]int
}

// param returns the parameter name for path under prefix, adding it.
func (g *sbplGen) param(prefix, path string) string {
	k := prefix + "\x00" + path
	if name, ok := g.keys[k]; ok {
		return name
	}
	if g.next == nil {
		g.next = map[string]int{}
	}
	name := fmt.Sprintf("%s_%d", prefix, g.next[prefix])
	g.next[prefix]++
	g.keys[k] = name
	g.params = append(g.params, name+"="+path)
	return name
}

// expandAll validates paths and returns every form of each, deduplicated, in
// input order.
func (g *sbplGen) expandAll(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		if err := validateSBPLPath(p); err != nil {
			return nil, err
		}
		for _, f := range g.forms(p) {
			if err := validateSBPLPath(f); err != nil {
				return nil, err
			}
			out = appendUnique(out, f)
		}
	}
	return out, nil
}

// forms returns p, its canonical form and the /private alias of each
// (Grok macos_deny_aliases).
func (g *sbplGen) forms(p string) []string {
	p = filepath.Clean(p)
	forms := []string{p}
	if g.opts.Canonical != nil {
		if c := g.opts.Canonical(p); c != "" {
			forms = appendUnique(forms, filepath.Clean(c))
		}
	}
	for _, f := range append([]string(nil), forms...) {
		if alias, ok := togglePrivatePrefix(f); ok {
			forms = appendUnique(forms, alias)
		}
	}
	return forms
}

// ancestorsWithin returns the parents of path from the deepest writable root
// containing it (inclusive) down to path's parent, keeping only existing
// ones. Empty when path is outside every root or is a root itself.
func (g *sbplGen) ancestorsWithin(path string, roots []string) []string {
	root := ""
	for _, r := range roots {
		if pathWithin(path, r) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" || root == path {
		return nil
	}
	var out []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if g.opts.Exists == nil || g.opts.Exists(dir) {
			out = append(out, dir)
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
	}
	// Outermost first, for a stable, readable profile.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// globRegexes translates a deny glob into anchored Seatbelt regex bodies, one
// per form of its literal root. The regex matches the glob's matches and
// everything below them.
func (g *sbplGen) globRegexes(glob string) ([]string, error) {
	if err := validateSBPLPath(glob); err != nil {
		return nil, err
	}
	if strings.Contains(glob, `\`) {
		return nil, fmt.Errorf("%w: backslash in deny glob %q", ErrSBPLPath, glob)
	}
	glob = filepath.Clean(glob)
	// Split into the literal root (components without metacharacters) and
	// the glob tail.
	parts := strings.Split(strings.TrimPrefix(glob, "/"), "/")
	i := 0
	for i < len(parts) && !sbplIsGlob(parts[i]) {
		i++
	}
	root := "/" + strings.Join(parts[:i], "/")
	tail, err := globTailToRegex(strings.Join(parts[i:], "/"))
	if err != nil {
		return nil, fmt.Errorf("%w: deny glob %q: %v", ErrSBPLPath, glob, err)
	}
	var out []string
	for _, form := range g.forms(root) {
		if err := validateSBPLPath(form); err != nil {
			return nil, err
		}
		prefix := regexp.QuoteMeta(strings.TrimSuffix(form, "/"))
		out = append(out, "^"+prefix+"/"+tail+"(/.*)?$")
	}
	return out, nil
}

// globTailToRegex translates a gitignore-style glob tail: "**" spans
// directories, "*" and "?" stay within one component, [...] classes are
// copied with a leading "!" turned into "^"; everything else is literal.
func globTailToRegex(tail string) (string, error) {
	var b strings.Builder
	rs := []rune(tail)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case '*':
			if i+1 < len(rs) && rs[i+1] == '*' {
				i++
				if i+1 < len(rs) && rs[i+1] == '/' {
					// "**/" matches zero or more directories.
					i++
					b.WriteString("(.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := i + 1
			if end < len(rs) && (rs[end] == '!' || rs[end] == '^') {
				end++
			}
			if end < len(rs) && rs[end] == ']' {
				end++
			}
			for end < len(rs) && rs[end] != ']' {
				end++
			}
			if end >= len(rs) {
				return "", errors.New("unterminated character class")
			}
			class := rs[i+1 : end]
			b.WriteByte('[')
			if len(class) > 0 && (class[0] == '!' || class[0] == '^') {
				b.WriteByte('^')
				class = class[1:]
			}
			b.WriteString(string(class))
			b.WriteByte(']')
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), nil
}

// validateSBPLPath rejects paths that cannot safely reach a profile: not
// absolute, or holding a control character (newline, NUL, ...) or a double
// quote. Silently passing one through would target a different path than
// intended (Grok deny/mod.rs escape_seatbelt_path).
func validateSBPLPath(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: not an absolute path: %q", ErrSBPLPath, p)
	}
	for _, r := range p {
		if unicode.IsControl(r) || r == '"' || r == unicode.ReplacementChar {
			return fmt.Errorf("%w: invalid character %q in %q", ErrSBPLPath, r, p)
		}
	}
	return nil
}

// togglePrivatePrefix maps /tmp, /var and /etc paths to their /private form
// and back (/private/tmp/x <-> /tmp/x). ok is false for other paths.
func togglePrivatePrefix(p string) (string, bool) {
	for _, dir := range []string{"/tmp", "/var", "/etc"} {
		if rest, ok := strings.CutPrefix(p, "/private"+dir); ok && (rest == "" || rest[0] == '/') {
			return dir + rest, true
		}
		if rest, ok := strings.CutPrefix(p, dir); ok && (rest == "" || rest[0] == '/') {
			return "/private" + dir + rest, true
		}
	}
	return "", false
}

// pathWithin reports whether p is root or below it (component-wise).
func pathWithin(p, root string) bool {
	if p == root || root == "/" {
		return true
	}
	return strings.HasPrefix(p, root+"/")
}

// sbplIsGlob reports whether a deny entry is a glob pattern.
func sbplIsGlob(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		dup := false
		for _, have := range list {
			if have == it {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, it)
		}
	}
	return list
}

// canonicalExisting resolves the symlinks of p's deepest existing ancestor and
// re-joins the missing rest, so a not-yet-created protected file under a
// symlinked workspace still gets its real-path form.
func canonicalExisting(p string) string {
	p = filepath.Clean(p)
	var rest []string
	for dir := p; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{real}, rest...)...)
		}
		if dir == filepath.Dir(dir) {
			return p
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
	}
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
