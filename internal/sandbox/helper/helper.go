// Package helper is the in-process half of the Linux sandbox: the
// `pando __sandbox-exec` re-exec helper. The parent (internal/sandbox, see
// wrapper_linux.go) rewrites a command to
//
//	<pando> __sandbox-exec --policy-fd N -- <argv...>
//
// optionally prefixed by bubblewrap, and passes a Spec as JSON on fd N. The
// helper confines its own thread (no_new_privs, Landlock, seccomp) and then
// execve()s the real command, which inherits the confinement.
//
// The helper must start fast and must never load Pando's configuration,
// logging or database. It therefore dispatches from this package's init
// function, which runs before the heavy packages of the binary are
// initialised, and this package imports only the standard library and
// golang.org/x/sys/unix (enforced by TestHelperImports). Any binary that links
// this package (pando itself, the desktop app, test binaries of packages that
// import internal/sandbox) can act as the helper.
package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Arg is the argv[1] that turns the binary into the sandbox helper.
const Arg = "__sandbox-exec"

// SpecVersion is the wire version of Spec. The helper refuses other versions
// (parent and helper are always the same binary, so a mismatch means a stale
// or foreign caller).
const SpecVersion = 1

// Exit codes of the helper itself (the exec'd command's own codes pass
// through untouched, since the helper is replaced by execve).
const (
	// ExitSetupFailed: the sandbox could not be applied; the command did not run.
	ExitSetupFailed = 126
	// ExitNotFound: the command to run does not exist.
	ExitNotFound = 127
)

// Network modes of Spec.Network.
const (
	// NetworkAllowed: no network filter.
	NetworkAllowed = "allowed"
	// NetworkRestricted: the seccomp network filter and, when the kernel
	// supports it, Landlock TCP and abstract-unix-socket restrictions.
	NetworkRestricted = "restricted"
)

// Spec is the resolved confinement the helper applies. The parent computes it
// from sandbox.Policy (which lives in a package that imports the config
// system, so it is not decoded here): the helper only turns lists into
// Landlock rules and a seccomp program.
type Spec struct {
	// V is SpecVersion.
	V int `json:"v"`
	// Path is the executable to run (exec.Cmd.Path). Empty means: look up
	// argv[0] in PATH.
	Path string `json:"path,omitempty"`
	// ReadDirs may be read and executed recursively ("/" means everything).
	ReadDirs []string `json:"readDirs,omitempty"`
	// WriteDirs may be read, written, created in and removed from
	// recursively.
	WriteDirs []string `json:"writeDirs,omitempty"`
	// WriteFiles are individual files that may be read and written (the
	// Landlock-only protected-path fallback grants the top-level files of a
	// writable root one by one).
	WriteFiles []string `json:"writeFiles,omitempty"`
	// Devices are device files and directories granted read/write/ioctl.
	// Missing ones, and ones that cannot be opened (a /dev/tty without a
	// controlling terminal), are skipped.
	Devices []string `json:"devices,omitempty"`
	// Network is NetworkAllowed or NetworkRestricted.
	Network string `json:"network"`
	// AllowUnixSockets keeps AF_UNIX usable under NetworkRestricted: only
	// socket(2) of other families (and io_uring) is refused. The parent sets
	// it only when it could mask the dangerous sockets (container runtimes,
	// D-Bus) with bubblewrap; otherwise every connect/bind/listen/accept and
	// addressed send is refused, whatever the family.
	AllowUnixSockets bool `json:"allowUnixSockets,omitempty"`
	// DenyConnectPorts are TCP ports the command must not connect to while
	// the network is allowed (Pando's own listeners). Enforced with Landlock
	// network rules when the kernel has them (ABI >= LandlockNetABI): every
	// other port is granted LANDLOCK_ACCESS_NET_CONNECT_TCP. Ignored under
	// NetworkRestricted, which already refuses every TCP connection.
	DenyConnectPorts []int `json:"denyConnectPorts,omitempty"`
	// PolicyHash is sandbox.Policy.Hash(), for diagnostics only.
	PolicyHash string `json:"policyHash,omitempty"`
}

// LandlockNetABI is the first Landlock ABI with TCP bind/connect rules
// (Linux 6.7).
const LandlockNetABI = 4

// RestrictsNetwork reports whether the network filter is requested.
func (s Spec) RestrictsNetwork() bool {
	return s.Network == NetworkRestricted
}

// Encode serialises the spec for the policy fd.
func (s Spec) Encode() ([]byte, error) {
	s.V = SpecVersion
	return json.Marshal(s)
}

// DecodeSpec parses and validates a spec read from the policy fd.
func DecodeSpec(data []byte) (Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return Spec{}, fmt.Errorf("decode sandbox spec: %w", err)
	}
	if s.V != SpecVersion {
		return Spec{}, fmt.Errorf("unsupported sandbox spec version %d (want %d)", s.V, SpecVersion)
	}
	switch s.Network {
	case NetworkAllowed, NetworkRestricted:
	default:
		return Spec{}, fmt.Errorf("unknown sandbox network mode %q", s.Network)
	}
	for _, port := range s.DenyConnectPorts {
		if port <= 0 || port > 65535 {
			return Spec{}, fmt.Errorf("invalid guarded port %d", port)
		}
	}
	return s, nil
}

// Args builds the helper's own arguments (after the executable):
// [Arg, "--policy-fd", N, "--", argv...].
func Args(policyFD int, argv []string) []string {
	out := make([]string, 0, 4+len(argv))
	out = append(out, Arg, "--policy-fd", strconv.Itoa(policyFD), "--")
	return append(out, argv...)
}

// ParseArgs parses the arguments that follow Arg: "--policy-fd N -- argv...".
func ParseArgs(args []string) (policyFD int, argv []string, err error) {
	policyFD = -1
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--policy-fd":
			if i+1 >= len(args) {
				return 0, nil, errors.New("--policy-fd needs a value")
			}
			n, convErr := strconv.Atoi(args[i+1])
			if convErr != nil || n < 3 {
				return 0, nil, fmt.Errorf("invalid --policy-fd %q", args[i+1])
			}
			policyFD = n
			i++
		case "--":
			argv = args[i+1:]
			if len(argv) == 0 {
				return 0, nil, errors.New("no command after --")
			}
			if policyFD < 0 {
				return 0, nil, errors.New("missing --policy-fd")
			}
			return policyFD, argv, nil
		default:
			return 0, nil, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return 0, nil, errors.New("missing -- before the command")
}

// readSpec reads and closes the policy fd.
func readSpec(fd int) (Spec, error) {
	f := os.NewFile(uintptr(fd), "sandbox-policy")
	if f == nil {
		return Spec{}, fmt.Errorf("invalid policy fd %d", fd)
	}
	defer f.Close()
	// memfd written by the parent: rewind in case the offset moved (the
	// parent shares the open file description).
	_, _ = f.Seek(0, io.SeekStart)
	data, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return Spec{}, fmt.Errorf("read policy fd %d: %w", fd, err)
	}
	return DecodeSpec(data)
}

// Run is the helper entry point: args are the arguments after Arg. On success
// it does not return (the process image is replaced by the command); on
// failure it returns the exit code after printing the reason on stderr.
func Run(args []string) int {
	fd, argv, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pando sandbox: %v\n", err)
		return ExitSetupFailed
	}
	spec, err := readSpec(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pando sandbox: %v\n", err)
		return ExitSetupFailed
	}
	return run(spec, argv)
}

// init dispatches the helper before any other package of the binary is
// initialised beyond this package's small dependency set (see the package
// doc). Go initialises packages in dependency order and, among ready ones, by
// import path, so this runs long before the config, logging and UI packages.
func init() {
	if len(os.Args) > 1 && os.Args[1] == Arg {
		os.Exit(Run(os.Args[2:]))
	}
}
