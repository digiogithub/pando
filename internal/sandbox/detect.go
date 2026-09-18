package sandbox

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DenialKind is what a sandbox denial blocked.
type DenialKind string

const (
	// DenialFS is a file-system access (write outside the writable roots, a
	// protected path, a denied path or, in strict mode, a read outside the
	// readable roots).
	DenialFS DenialKind = "fs"
	// DenialNet is a network access while the policy restricts the network.
	DenialNet DenialKind = "net"
)

// Operation classifies the file-system access a denial was about.
const (
	OpWrite = "write"
	OpRead  = "read"
)

// Denial is the result of Classify: a failed command whose output looks like
// the sandbox, not the command itself, refused an operation.
type Denial struct {
	Kind DenialKind `json:"kind"`
	// Evidence is the output line the decision was based on (trimmed).
	Evidence string `json:"evidence"`
	// Path is the absolute path the failing operation referred to, when one
	// could be extracted from the output.
	Path string `json:"path,omitempty"`
	// Op is OpWrite or OpRead when the output tells which one failed; empty
	// when unknown or for network denials.
	Op string `json:"op,omitempty"`
	// WorkspaceRootEntry is true when the path is a new entry directly in the
	// workspace root. The Landlock-only backend (no bubblewrap) grants a
	// writable root that contains protected paths entry by entry, so creating
	// new top-level files there fails although the workspace is writable.
	WorkspaceRootEntry bool `json:"workspaceRootEntry,omitempty"`
}

// maxEvidence caps Denial.Evidence.
const maxEvidence = 240

// hostAccess reports whether the Pando process itself (outside any sandbox)
// can access path for writing (write=true) or reading. known is false when the
// check is not possible (e.g. on Windows); a missing path is checked through
// its nearest existing ancestor. A variable so tests can fake the host's view.
var hostAccess = defaultHostAccess

// Classify reports whether a failed command's output looks like a sandbox
// denial under policy p. Relative paths in the output are resolved against
// p.Workspace; use ClassifyAt when the command ran in another directory.
//
// The caller must only classify runs that were really confined (policy
// covering the spawn site and an enforced backend): Classify cannot tell and
// would otherwise blame the sandbox for ordinary permission errors.
func Classify(exitCode int, output string, p Policy) (Denial, bool) {
	return ClassifyAt(exitCode, output, p, p.Workspace)
}

// ClassifyAt is Classify resolving relative paths against cwd.
//
// Heuristics (sandbox denials are only inferred, see the package doc):
//   - A zero exit code or a disabled policy is never a denial.
//   - Network errors ("Network is unreachable", "Could not resolve host",
//     "Temporary failure in name resolution", "connect: operation not
//     permitted", ...) count only while the policy restricts the network.
//   - File errors ("Permission denied", "Operation not permitted",
//     "Read-only file system", EACCES/EPERM/EROFS, Seatbelt "deny(1)
//     file-write") are cross-checked against the policy and the host's own
//     access to the path mentioned: a path inside a writable root that is not
//     protected, or one the host user cannot access either, is an ordinary
//     permission problem, not the sandbox.
func ClassifyAt(exitCode int, output string, p Policy, cwd string) (Denial, bool) {
	if exitCode == 0 || !p.Enabled() || strings.TrimSpace(output) == "" {
		return Denial{}, false
	}
	if cwd == "" {
		cwd = p.Workspace
	}
	lines := strings.Split(output, "\n")

	if p.RestrictsNetwork() {
		for _, line := range lines {
			if netErrorRe.MatchString(line) {
				return Denial{Kind: DenialNet, Evidence: evidence(line)}, true
			}
		}
	}

	// Seatbelt reports its own denials (e.g. when the log is echoed or a tool
	// prints the kernel message): they are the sandbox by definition.
	for _, line := range lines {
		if m := seatbeltDenyRe.FindStringSubmatch(line); m != nil {
			op := OpRead
			if strings.HasPrefix(strings.ToLower(m[1]), "file-write") {
				op = OpWrite
			}
			path := cleanAbs(strings.TrimSpace(m[2]), cwd)
			if len(path) > 0 && !filepath.IsAbs(path) {
				path = ""
			}
			return Denial{Kind: DenialFS, Evidence: evidence(line), Path: path, Op: op}, true
		}
	}

	npmPath, npmOp := npmPathFrom(lines, cwd)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !fsErrorRe.MatchString(line) || falsePositiveRe.MatchString(line) {
			continue
		}
		rofs := rofsRe.MatchString(line)
		path := pathFrom(line, cwd)
		op := opFrom(lower)
		if path == "" && (strings.Contains(lower, "eacces") || strings.Contains(lower, "eperm")) {
			path = npmPath
			if op == "" {
				op = npmOp
			}
		}
		if rofs {
			op = OpWrite
		}
		d := Denial{Kind: DenialFS, Evidence: evidence(line), Path: path, Op: op}
		if path == "" {
			// Without a path only a read-only file system is specific enough:
			// the bwrap backend mounts protected paths read-only, and a real
			// read-only mount is rare on a developer machine.
			if rofs {
				return d, true
			}
			continue
		}
		if exitCode == 126 && notExecutable(path) {
			// "bash: ./x: Permission denied" with 126: the file exists but
			// is not executable, an ordinary mode problem.
			continue
		}
		if denied, rootEntry := fsDenied(path, op, rofs, p); denied {
			d.WorkspaceRootEntry = rootEntry
			return d, true
		}
	}
	return Denial{}, false
}

// localizedROFS is the EROFS strerror text in the locales fsErrorRe covers.
const localizedROFS = `sistema de ficheros de sólo lectura|sistema de archivos de solo lectura|système de fichiers accessible en lecture seulement|nur lesbares dateisystem|dateisystem ist nur lesbar|file system di sola lettura|sistema de arquivos somente para leitura`

var (
	// netErrorRe matches network failures a restricted network produces.
	netErrorRe = regexp.MustCompile(`(?i)(network is unreachable|could not resolve host|temporary failure in name resolution|name or service not known|no address associated with hostname|getaddrinfo (eai_again|enotfound)|(connect|socket|dial tcp[^:]*|dial udp[^:]*|sendto|bind)[^\n]{0,80}: operation not permitted|failed to establish a new connection|could not resolve proxy|unable to access '[^']*': could not resolve)`)

	// fsErrorRe matches file access errors: the C-locale strerror texts plus
	// their glibc translations for common locales (es, fr, de, it, pt), since
	// coreutils print localized messages under the user's LANG.
	fsErrorRe = regexp.MustCompile(`(?i)(permission denied|operation not permitted|read-only file system|\beacces\b|\beperm\b|\berofs\b|` +
		`permiso denegado|operación no permitida|permission non accordée|opération non permise|keine berechtigung|vorgang nicht zulässig|operation nicht erlaubt|permesso negato|operazione non permessa|permissão negada|operação não permitida|` +
		localizedROFS + `)`)

	// rofsRe matches a read-only file system error (EROFS), localized too.
	rofsRe = regexp.MustCompile(`(?i)(read-only file system|\berofs\b|` + localizedROFS + `)`)

	// falsePositiveRe matches permission errors that are never the sandbox's
	// file rules: SSH/HTTP authentication, the Docker daemon socket, sudo.
	falsePositiveRe = regexp.MustCompile(`(?i)(permission denied \((publickey|password|keyboard-interactive|gssapi)|permission denied, please try again|docker daemon socket|docker\.sock|http(s)? 40[13]|access denied for user|sudo: |ssh: |git@|remote: permission)`)

	// seatbeltDenyRe matches a Seatbelt violation line ("deny(1) file-write-create /x").
	seatbeltDenyRe = regexp.MustCompile(`(?i)\bdeny\(\d+\)\s+(file-(?:write|read)[a-z-]*)\s*(\S*)`)

	// quotedRe captures quoted strings (ASCII, typographic and backtick quotes).
	quotedRe = regexp.MustCompile("['\"`‘“]([^'\"`’”\n]+)['\"`’”]")

	// npmSyscallRe captures npm's "npm ERR! syscall mkdir".
	npmSyscallRe = regexp.MustCompile(`(?i)^npm (?:err!|error) syscall (\w+)`)

	// npmPathRe captures npm's "npm ERR! path /x" / "npm error path /x".
	npmPathRe = regexp.MustCompile(`(?i)^npm (?:err!|error) path (.+)$`)

	// writeOpRe / readOpRe guess the failing operation from the message.
	writeOpRe = regexp.MustCompile(`(?i)(^\s*(touch|mkdir|rm|rmdir|mv|ln|chmod|chown|tee|truncate|install):|cannot (create|touch|remove|make|move|write|overwrite|open .* for writing|mkdir)|could not (lock|create|write|open .* for writing|remove|rename)|unable to (create|write|unlink|remove|rename|lock)|failed to (create|write|remove|rename|lock)|\b(mkdir|rmdir|unlink|rename|symlink|link|copyfile|chmod|chown|utime|open .*'w)\b|\beacces: permission denied, (mkdir|open|rename|unlink|symlink|copyfile|rmdir|chmod)|read-only file system|\berofs\b|file-write|errno 30)`)
	readOpRe  = regexp.MustCompile(`(?i)(^\s*(cat|less|more|head|tail|grep|rg|ls|find|stat|du|wc|diff|sort|file|source|\.):|cannot (open|read|access|stat)|could not (open|read)|unable to (open|read)|failed to (open|read)|\beacces: permission denied, (scandir|stat|lstat|open)|file-read)`)
)

func evidence(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > maxEvidence {
		line = line[:maxEvidence] + "..."
	}
	return line
}

// opFrom guesses whether the failed operation was a write or a read.
func opFrom(lower string) string {
	switch {
	case writeOpRe.MatchString(lower):
		return OpWrite
	case readOpRe.MatchString(lower):
		return OpRead
	}
	return ""
}

// pathFrom extracts the path an error line refers to: a quoted path first,
// then an unquoted token ending in ':' before the error ("x: Permission
// denied"), then any absolute token. Relative paths resolve against cwd.
func pathFrom(line, cwd string) string {
	// Prefer a quoted string that looks like a path: localized messages quote
	// the program name too ("touch: no se puede efectuar `touch' sobre '/x'").
	first := ""
	for _, m := range quotedRe.FindAllStringSubmatch(line, -1) {
		if c := candidatePath(m[1]); c != "" {
			if strings.ContainsAny(c, "/~") {
				return cleanAbs(c, cwd)
			}
			if first == "" {
				first = c
			}
		}
	}
	if first != "" {
		return cleanAbs(first, cwd)
	}
	// "prog: /path: Permission denied", "touch: foo: Operation not permitted"
	idx := fsErrorRe.FindStringIndex(line)
	if idx != nil {
		head := strings.TrimRight(line[:idx[0]], " ")
		head = strings.TrimSuffix(head, ":")
		head = strings.TrimSuffix(head, ",")
		fields := strings.Fields(head)
		if n := len(fields); n > 0 {
			last := strings.TrimSuffix(fields[n-1], ":")
			// A bare program name ("bash:") is not a path; require a slash,
			// a dot or a second colon-separated segment.
			if c := candidatePath(last); c != "" && (strings.ContainsAny(c, "/.~") || strings.Count(head, ":") >= 2) && !isProgramPrefix(fields) {
				return cleanAbs(c, cwd)
			}
		}
	}
	for _, f := range strings.Fields(line) {
		f = strings.Trim(f, `:,;()[]'"`)
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "~/") {
			if c := candidatePath(f); c != "" {
				return cleanAbs(c, cwd)
			}
		}
	}
	return ""
}

// isProgramPrefix reports whether the only token before the error is the
// program name ("mkdir: Permission denied"), which is not a path.
func isProgramPrefix(fields []string) bool {
	return len(fields) == 1 && !strings.ContainsAny(fields[0], "/.~")
}

// candidatePath returns s when it looks like a file path.
func candidatePath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ":")
	if s == "" || len(s) > 4096 || strings.ContainsAny(s, "\x00\n") {
		return ""
	}
	if strings.Contains(s, "://") || strings.HasPrefix(s, "-") {
		return ""
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return s
	}
	// Relative names: no spaces (messages, not paths) and a plausible file name.
	if strings.ContainsAny(s, " \t") {
		return ""
	}
	return s
}

// cleanAbs makes p absolute (relative to cwd, "~" to the home directory).
func cleanAbs(p, cwd string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		if cwd == "" {
			return ""
		}
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// npmPathFrom returns the path of npm's "npm ERR! path" line and the
// operation its "npm ERR! syscall" line implies, if any.
func npmPathFrom(lines []string, cwd string) (path, op string) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := npmPathRe.FindStringSubmatch(line); m != nil && path == "" {
			path = cleanAbs(strings.TrimSpace(m[1]), cwd)
		}
		if m := npmSyscallRe.FindStringSubmatch(line); m != nil && op == "" {
			op = opFrom("eacces: permission denied, " + strings.ToLower(m[1]))
		}
	}
	return path, op
}

// fsDenied cross-checks a failed access to path against the policy and the
// host's own access. rootEntry reports the Landlock workspace-root case.
func fsDenied(path, op string, rofs bool, p Policy) (denied, rootEntry bool) {
	if matchesAny(path, p.DenyPaths) {
		return true, false
	}
	if op != OpRead && withinAny(path, p.ProtectedPaths) {
		return true, false
	}
	if op == OpRead {
		// Reads are only confined in strict mode (ReadableRoots) and by
		// DenyPaths (checked above).
		if len(p.ReadableRoots) == 0 || withinAny(path, p.ReadableRoots) {
			return false, false
		}
		can, known := hostAccess(path, false)
		return !known || can, false
	}
	// A write, or an unknown operation.
	if withinAny(path, p.WritableRoots) {
		if rofs {
			// Nothing inside a writable root is mounted read-only except
			// protected paths, and bwrap mounts them so.
			return true, false
		}
		// Inside a writable root the sandbox only blocks what the host can
		// write but the backend does not grant: the workspace-root entries
		// of the Landlock-only backend.
		can, known := hostAccess(path, true)
		if !known || !can {
			return false, false
		}
		parent := filepath.Dir(path)
		rootEntry = p.Workspace != "" && parent == filepath.Clean(p.Workspace) && !exists(path)
		return true, rootEntry
	}
	// Outside the writable roots: a write the host could do is the sandbox;
	// one the host cannot do either is an ordinary permission error.
	can, known := hostAccess(path, true)
	if !known {
		return op == OpWrite || rofs, false
	}
	if can {
		return true, false
	}
	if op == "" && len(p.ReadableRoots) > 0 && !withinAny(path, p.ReadableRoots) {
		// Strict mode: an unknown operation on a path outside the readable
		// roots that the host can read is most likely a blocked read.
		canRead, knownRead := hostAccess(path, false)
		return knownRead && canRead, false
	}
	return false, false
}

// withinAny reports whether path is one of roots or below one (component-wise).
func withinAny(path string, roots []string) bool {
	for _, r := range roots {
		if r == "" || strings.ContainsAny(r, "*?[") {
			continue
		}
		if pathWithinRoot(path, filepath.Clean(r)) {
			return true
		}
	}
	return false
}

// matchesAny reports whether path is within one of the deny entries, which
// may be globs (matched against the path and each of its ancestors).
func matchesAny(path string, entries []string) bool {
	for _, e := range entries {
		if e == "" {
			continue
		}
		if !strings.ContainsAny(e, "*?[") {
			if pathWithinRoot(path, filepath.Clean(e)) {
				return true
			}
			continue
		}
		for dir := path; ; dir = filepath.Dir(dir) {
			if ok, _ := filepath.Match(e, dir); ok {
				return true
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	return false
}

func pathWithinRoot(p, root string) bool {
	if p == root {
		return true
	}
	sep := string(filepath.Separator)
	if strings.HasSuffix(root, sep) {
		return strings.HasPrefix(p, root)
	}
	return strings.HasPrefix(p, root+sep)
}

// notExecutable reports whether p is an existing regular file without any
// execute bit.
func notExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 == 0
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// nearestExisting returns path or its deepest ancestor that can be stat'ed.
func nearestExisting(path string) (string, bool) {
	for dir := path; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); err == nil {
			return dir, true
		}
		if dir == filepath.Dir(dir) {
			return "", false
		}
	}
}
