//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHost fakes the host's own access for Classify: every path is readable
// and writable except those under the listed prefixes.
func fakeHost(t *testing.T, noWrite, noRead []string) {
	t.Helper()
	prev := hostAccess
	hostAccess = func(path string, write bool) (bool, bool) {
		list := noRead
		if write {
			list = noWrite
		}
		for _, p := range list {
			if pathWithinRoot(path, p) {
				return false, true
			}
		}
		return true, true
	}
	t.Cleanup(func() { hostAccess = prev })
}

func TestClassify(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "ro.txt"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "script.sh"), []byte("echo"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeHost(t,
		[]string{"/etc", "/usr", "/root", filepath.Join(ws, "ro.txt")},
		[]string{"/root", "/etc/shadow"},
	)

	base := Policy{
		Mode:           ModeWorkspaceWrite,
		Network:        NetworkAllowed,
		Workspace:      ws,
		WritableRoots:  []string{ws, "/tmp", "/home/u/.cache"},
		ProtectedPaths: []string{filepath.Join(ws, ".pando"), filepath.Join(ws, ".git", "hooks"), filepath.Join(ws, ".git", "config")},
		DenyPaths:      []string{"/home/u/.ssh", "/home/u/.aws"},
	}
	restricted := base
	restricted.Network = NetworkRestricted
	strict := base
	strict.Mode = ModeStrict
	strict.Network = NetworkRestricted
	strict.ReadableRoots = []string{ws, "/usr", "/bin", "/etc"}
	off := base
	off.Mode = ModeOff

	cases := []struct {
		name     string
		exit     int
		out      string
		p        Policy
		want     bool
		kind     DenialKind
		path     string
		op       string
		rootNew  bool
		evidence string
	}{
		// Linux, file system.
		{name: "touch outside workspace", exit: 1, out: "touch: cannot touch '/home/u/.bashrc': Permission denied", p: base, want: true, kind: DenialFS, path: "/home/u/.bashrc", op: OpWrite},
		{name: "mkdir typographic quotes", exit: 1, out: "mkdir: cannot create directory ‘/home/u/foo’: Permission denied", p: base, want: true, kind: DenialFS, path: "/home/u/foo", op: OpWrite},
		{name: "shell redirection", exit: 1, out: "bash: /home/u/out.txt: Permission denied", p: base, want: true, kind: DenialFS, path: "/home/u/out.txt"},
		{name: "protected path bwrap EROFS", exit: 1, out: "touch: cannot touch '" + ws + "/.pando/x': Read-only file system", p: base, want: true, kind: DenialFS, path: ws + "/.pando/x", op: OpWrite},
		{name: "protected git hook EACCES", exit: 1, out: "rm: cannot remove '" + ws + "/.git/hooks/pre-commit': Permission denied", p: base, want: true, kind: DenialFS, path: ws + "/.git/hooks/pre-commit", op: OpWrite},
		{name: "git lock config", exit: 255, out: "error: could not lock config file " + ws + "/.git/config: Permission denied", p: base, want: true, kind: DenialFS, path: ws + "/.git/config", op: OpWrite},
		{name: "npm EACCES outside workspace", exit: 243, out: "npm ERR! code EACCES\nnpm ERR! syscall mkdir\nnpm ERR! path /home/u/.npm-global/lib/node_modules/x\nnpm ERR! errno -13\nnpm ERR! Error: EACCES: permission denied, mkdir '/home/u/.npm-global/lib/node_modules/x'", p: base, want: true, kind: DenialFS, path: "/home/u/.npm-global/lib/node_modules/x", op: OpWrite},
		{name: "python PermissionError", exit: 1, out: "Traceback (most recent call last):\nPermissionError: [Errno 13] Permission denied: '/home/u/data.json'", p: base, want: true, kind: DenialFS, path: "/home/u/data.json"},
		{name: "landlock workspace root entry", exit: 1, out: "touch: cannot touch 'newfile.txt': Permission denied", p: base, want: true, kind: DenialFS, path: ws + "/newfile.txt", op: OpWrite, rootNew: true},
		{name: "deny path read", exit: 1, out: "cat: /home/u/.ssh/id_rsa: Permission denied", p: base, want: true, kind: DenialFS, path: "/home/u/.ssh/id_rsa"},
		{name: "strict read outside readable roots", exit: 1, out: "cat: /home/u/notes.txt: Permission denied", p: strict, want: true, kind: DenialFS, path: "/home/u/notes.txt"},
		{name: "pathless read-only file system", exit: 1, out: "cp: error writing: Read-only file system", p: base, want: true, kind: DenialFS, op: OpWrite},
		// Localized coreutils/bash messages (LANG=es_ES, de_DE, fr_FR).
		{name: "touch outside workspace, Spanish", exit: 1, out: "touch: no se puede efectuar `touch' sobre '/home/u/.bashrc': Permiso denegado", p: base, want: true, kind: DenialFS, path: "/home/u/.bashrc"},
		{name: "redirection, German", exit: 1, out: "bash: /home/u/out.txt: Keine Berechtigung", p: base, want: true, kind: DenialFS, path: "/home/u/out.txt"},
		{name: "protected path EROFS, French", exit: 1, out: "touch: impossible de faire un touch '" + ws + "/.pando/x': Système de fichiers accessible en lecture seulement", p: base, want: true, kind: DenialFS, path: ws + "/.pando/x", op: OpWrite},
		{name: "host cannot write either, Spanish", exit: 1, out: "touch: no se puede efectuar `touch' sobre '/usr/bin/x': Permiso denegado", p: base},
		// Linux, network (restricted).
		{name: "curl resolve restricted", exit: 6, out: "curl: (6) Could not resolve host: example.com", p: restricted, want: true, kind: DenialNet},
		{name: "network unreachable restricted", exit: 2, out: "ping: connect: Network is unreachable", p: restricted, want: true, kind: DenialNet},
		{name: "go socket EPERM", exit: 1, out: "Get \"https://proxy.golang.org/x\": dial tcp 142.250.1.1:443: socket: operation not permitted", p: restricted, want: true, kind: DenialNet},
		{name: "pip name resolution", exit: 1, out: "WARNING: Retrying ... NewConnectionError: Failed to establish a new connection: [Errno -3] Temporary failure in name resolution", p: strict, want: true, kind: DenialNet},
		{name: "npm EAI_AGAIN", exit: 1, out: "npm ERR! request to https://registry.npmjs.org/x failed, reason: getaddrinfo EAI_AGAIN registry.npmjs.org", p: restricted, want: true, kind: DenialNet},
		{name: "git clone resolve", exit: 128, out: "fatal: unable to access 'https://github.com/a/b.git/': Could not resolve host: github.com", p: restricted, want: true, kind: DenialNet},
		// macOS (Seatbelt returns EPERM).
		{name: "macos touch EPERM", exit: 1, out: "touch: /Users/u/foo: Operation not permitted", p: base, want: true, kind: DenialFS, path: "/Users/u/foo", op: OpWrite},
		{name: "macos seatbelt log write", exit: 1, out: "Sandbox: bash(4242) deny(1) file-write-create /Users/u/foo", p: base, want: true, kind: DenialFS, path: "/Users/u/foo", op: OpWrite},
		{name: "macos seatbelt log read", exit: 1, out: "Sandbox: cat(4243) deny(1) file-read-data /Users/u/secret", p: strict, want: true, kind: DenialFS, path: "/Users/u/secret", op: OpRead},
		{name: "macos git clone EPERM", exit: 128, out: "fatal: could not create work tree dir '/Users/u/proj': Operation not permitted", p: base, want: true, kind: DenialFS, path: "/Users/u/proj", op: OpWrite},
		{name: "macos protected pando config", exit: 1, out: "sed: " + ws + "/.pando/config.toml: Operation not permitted", p: base, want: true, kind: DenialFS, path: ws + "/.pando/config.toml"},
		// False positives.
		{name: "ssh publickey", exit: 255, out: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", p: base},
		{name: "docker socket", exit: 1, out: "Got permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: dial unix /var/run/docker.sock: connect: permission denied", p: base},
		{name: "host cannot read either", exit: 1, out: "cat: /etc/shadow: Permission denied", p: base},
		{name: "host cannot write either", exit: 1, out: "touch: cannot touch '/usr/bin/x': Permission denied", p: base},
		{name: "npm EACCES global prefix host-owned", exit: 243, out: "npm ERR! path /usr/lib/node_modules/x\nnpm ERR! Error: EACCES: permission denied, mkdir '/usr/lib/node_modules/x'", p: base},
		{name: "read-only file in workspace", exit: 1, out: "bash: " + ws + "/ro.txt: Permission denied", p: base},
		{name: "not executable script", exit: 126, out: "bash: ./script.sh: Permission denied", p: base},
		{name: "resolve host with network allowed", exit: 6, out: "curl: (6) Could not resolve host: example.com", p: base},
		{name: "unreachable with network allowed", exit: 2, out: "ping: connect: Network is unreachable", p: base},
		{name: "zero exit code", exit: 0, out: "touch: cannot touch '/home/u/x': Permission denied", p: base},
		{name: "policy off", exit: 1, out: "touch: cannot touch '/home/u/x': Permission denied", p: off},
		{name: "pathless permission denied", exit: 1, out: "mkdir: Permission denied", p: base},
		{name: "remote git permission", exit: 128, out: "remote: Permission to foo/bar.git denied to someone.\nfatal: unable to access 'https://github.com/foo/bar.git/': The requested URL returned error: 403", p: base},
		{name: "sudo", exit: 1, out: "sudo: a terminal is required to read the password; either use the -S option", p: base},
		{name: "ordinary failure", exit: 1, out: "go: cannot find main module, but found .git/config in /x", p: base},
		{name: "read in workspace-write mode", exit: 1, out: "cat: /home/u/notes.txt: Permission denied", p: base},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := Classify(tc.exit, tc.out, tc.p)
			if ok != tc.want {
				t.Fatalf("Classify ok = %v, want %v (denial %+v)", ok, tc.want, d)
			}
			if !ok {
				return
			}
			if d.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", d.Kind, tc.kind)
			}
			if tc.path != "" && d.Path != tc.path {
				t.Errorf("path = %q, want %q", d.Path, tc.path)
			}
			if tc.op != "" && d.Op != tc.op {
				t.Errorf("op = %q, want %q", d.Op, tc.op)
			}
			if d.WorkspaceRootEntry != tc.rootNew {
				t.Errorf("workspace root entry = %v, want %v", d.WorkspaceRootEntry, tc.rootNew)
			}
			if d.Evidence == "" || strings.Contains(d.Evidence, "\n") {
				t.Errorf("evidence = %q, want one non-empty line", d.Evidence)
			}
		})
	}
}

func TestClassifyAtResolvesRelativeToCwd(t *testing.T) {
	ws := t.TempDir()
	fakeHost(t, nil, nil)
	p := Policy{Mode: ModeWorkspaceWrite, Workspace: ws, WritableRoots: []string{ws},
		ProtectedPaths: []string{filepath.Join(ws, "sub", ".pando")}}

	d, ok := ClassifyAt(1, "touch: cannot touch '.pando/x': Read-only file system", p, filepath.Join(ws, "sub"))
	if !ok || d.Path != filepath.Join(ws, "sub", ".pando", "x") {
		t.Fatalf("ClassifyAt = %+v, %v; want a denial on sub/.pando/x", d, ok)
	}
	if d.WorkspaceRootEntry {
		t.Fatal("a nested path is not a workspace root entry")
	}
}

func TestClassifyExistingWorkspaceEntryIsNotRootEntry(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHost(t, nil, nil)
	p := Policy{Mode: ModeWorkspaceWrite, Workspace: ws, WritableRoots: []string{ws}}

	d, ok := Classify(1, "rmdir: failed to remove '"+ws+"/dir': Permission denied", p)
	if !ok || d.WorkspaceRootEntry {
		t.Fatalf("Classify = %+v, %v; want a denial that is not a new root entry", d, ok)
	}
}

func TestClassifyEvidenceIsCapped(t *testing.T) {
	fakeHost(t, nil, nil)
	p := Policy{Mode: ModeWorkspaceWrite, Network: NetworkRestricted, Workspace: "/ws", WritableRoots: []string{"/ws"}}
	d, ok := Classify(1, "curl: (6) Could not resolve host: "+strings.Repeat("a", 1000), p)
	if !ok || len(d.Evidence) > maxEvidence+3 {
		t.Fatalf("evidence length = %d, want <= %d", len(d.Evidence), maxEvidence+3)
	}
}
