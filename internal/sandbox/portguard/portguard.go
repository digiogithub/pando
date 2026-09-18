// Package portguard keeps the registry of TCP ports that belong to Pando
// itself: the HTTP API, the AG-UI listener, the MCP HTTP server, the LLM
// proxy, the IPC bus, OAuth callbacks, the browser's DevTools port, ... A
// sandboxed command must never reach them: every one of those listeners can
// change the configuration (turning the sandbox off), run commands outside
// the sandbox or hand out credentials, and most of them trust loopback
// callers. internal/sandbox reads Ports into Policy.DenyConnectPorts and the
// backends refuse TCP connections to those ports.
//
// The registry has two halves:
//
//   - an in-process one (Register / Guard), always used;
//   - a shared one: each Pando process mirrors its own ports into
//     <global config dir>/run/ports/<pid>.json, and Ports also returns the
//     ports of every other live Pando process. The global config directory is
//     a protected path of every sandbox policy, so a sandboxed command cannot
//     remove an entry. This covers other instances on the same machine: an
//     IPC primary, a `pando serve` started by the Projects feature, the
//     desktop app, another project's TUI.
//
// This package is a leaf (standard library only) so that any package with a
// listener can use it without an import cycle; internal/sandbox re-exports
// RegisterGuardedPort / GuardedPorts.
package portguard

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// entry is one registration. Several may exist for the same port (a rebind
// registers the new listener before the old one is released).
type entry struct {
	port  int
	owner string
}

var (
	mu      sync.Mutex
	nextID  uint64
	entries = map[uint64]entry{}

	// sharedDir overrides the shared directory (tests); sharedSet records
	// that it was set explicitly, so "" can mean "disabled".
	sharedDir string
	sharedSet bool
)

// Register records port as belonging to this Pando process. owner is a short
// label for diagnostics ("api", "ipc-pub", ...). The returned function
// removes the registration; it is safe to call more than once. Ports outside
// 1-65535 are ignored.
func Register(port int, owner string) (unregister func()) {
	if port <= 0 || port > 65535 {
		return func() {}
	}
	mu.Lock()
	nextID++
	id := nextID
	entries[id] = entry{port: port, owner: owner}
	mu.Unlock()
	writeShared()

	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(entries, id)
			mu.Unlock()
			writeShared()
		})
	}
}

// RegisterAddr registers the TCP port of addr (a net.Addr from a listener).
// Non-TCP addresses are ignored.
func RegisterAddr(addr net.Addr, owner string) (unregister func()) {
	if tcp, ok := addr.(*net.TCPAddr); ok && tcp != nil {
		return Register(tcp.Port, owner)
	}
	if addr != nil {
		if _, portStr, err := net.SplitHostPort(addr.String()); err == nil {
			if port, err := strconv.Atoi(portStr); err == nil {
				return Register(port, owner)
			}
		}
	}
	return func() {}
}

// Guard registers the port of l and returns a listener whose Close also
// removes the registration. The returned listener forwards everything else to
// l unchanged.
func Guard(l net.Listener, owner string) net.Listener {
	if l == nil {
		return nil
	}
	return &guardedListener{Listener: l, unregister: RegisterAddr(l.Addr(), owner)}
}

type guardedListener struct {
	net.Listener
	unregister func()
}

func (g *guardedListener) Close() error {
	g.unregister()
	return g.Listener.Close()
}

// LocalPorts returns the ports registered by this process, sorted and
// deduplicated.
func LocalPorts() []int {
	mu.Lock()
	defer mu.Unlock()
	return localLocked()
}

// LocalOwners returns port -> owners registered by this process, for
// diagnostics (pando sandbox status).
func LocalOwners() map[int][]string {
	mu.Lock()
	defer mu.Unlock()
	out := map[int][]string{}
	for _, e := range entries {
		if !slices.Contains(out[e.port], e.owner) {
			out[e.port] = append(out[e.port], e.owner)
		}
	}
	for p := range out {
		slices.Sort(out[p])
	}
	return out
}

func localLocked() []int {
	out := make([]int, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.port)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Ports returns every guarded port: this process's registrations plus the
// ones published by other live Pando processes. Sorted and deduplicated.
func Ports() []int {
	out := LocalPorts()
	out = append(out, readShared()...)
	slices.Sort(out)
	return slices.Compact(out)
}

// SetSharedDirForTests points the shared registry at dir ("" disables it)
// until the returned function is called. By default the shared registry is
// disabled inside `go test` binaries so tests never write into the real home
// directory.
func SetSharedDirForTests(dir string) (restore func()) {
	mu.Lock()
	prevDir, prevSet := sharedDir, sharedSet
	sharedDir, sharedSet = dir, true
	mu.Unlock()
	return func() {
		mu.Lock()
		sharedDir, sharedSet = prevDir, prevSet
		mu.Unlock()
	}
}

// ResetForTests drops every in-process registration.
func ResetForTests() {
	mu.Lock()
	entries = map[uint64]entry{}
	mu.Unlock()
}

// SharedDir returns the shared registry directory, "" when disabled.
func SharedDir() string {
	mu.Lock()
	dir, set := sharedDir, sharedSet
	mu.Unlock()
	if set {
		return dir
	}
	if testing.Testing() {
		return ""
	}
	return defaultSharedDir()
}

// defaultSharedDir mirrors the sandbox's global config directory (the first
// entry of internal/sandbox globalConfigDirs), which is a protected path.
func defaultSharedDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "pando", "run", "ports")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "pando", "run", "ports")
}

// sharedFile is the on-disk form of one process's registrations.
type sharedFile struct {
	PID   int   `json:"pid"`
	Ports []int `json:"ports"`
}

// writeMu serialises writeShared, so concurrent registrations cannot leave
// an older snapshot on disk.
var writeMu sync.Mutex

// writeShared publishes this process's current ports (removing the file when
// there are none). Errors are ignored: the in-process registry still protects
// this process's own commands.
func writeShared() {
	dir := SharedDir()
	if dir == "" {
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	ports := LocalPorts()
	pid := os.Getpid()
	path := filepath.Join(dir, strconv.Itoa(pid)+".json")
	if len(ports) == 0 {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	data, err := json.Marshal(sharedFile{PID: pid, Ports: ports})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".ports-*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// readShared returns the ports published by other live processes. Files of
// dead processes are removed on the way.
func readShared() []int {
	dir := SharedDir()
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var out []int
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		path := filepath.Join(dir, name)
		if !processAlive(pid) {
			_ = os.Remove(path)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var f sharedFile
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		for _, p := range f.Ports {
			if p > 0 && p <= 65535 {
				out = append(out, p)
			}
		}
	}
	return out
}
