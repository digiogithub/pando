package helper

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSpecRoundTrip(t *testing.T) {
	in := Spec{
		Path:             "/usr/bin/bash",
		ReadDirs:         []string{"/"},
		WriteDirs:        []string{"/ws", "/tmp"},
		WriteFiles:       []string{"/ws/README.md"},
		Devices:          []string{"/dev/null", "/dev/tty"},
		Network:          NetworkRestricted,
		AllowUnixSockets: true,
		PolicyHash:       "abc",
	}
	data, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	in.V = SpecVersion
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in: %+v\nout: %+v", in, out)
	}
	if !out.RestrictsNetwork() {
		t.Fatal("RestrictsNetwork = false")
	}
}

func TestDecodeSpecRejects(t *testing.T) {
	for name, data := range map[string]string{
		"garbage":       "{",
		"no version":    `{"network":"allowed"}`,
		"wrong version": `{"v":99,"network":"allowed"}`,
		"bad network":   `{"v":1,"network":"sometimes"}`,
	} {
		if _, err := DecodeSpec([]byte(data)); err == nil {
			t.Errorf("%s: DecodeSpec accepted %s", name, data)
		}
	}
}

func TestArgsParseArgs(t *testing.T) {
	argv := []string{"bash", "-c", "echo -- --policy-fd"}
	args := Args(5, argv)
	want := []string{Arg, "--policy-fd", "5", "--", "bash", "-c", "echo -- --policy-fd"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Args = %q, want %q", args, want)
	}
	fd, got, err := ParseArgs(args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if fd != 5 || !reflect.DeepEqual(got, argv) {
		t.Fatalf("ParseArgs = %d %q", fd, got)
	}

	for _, bad := range [][]string{
		nil,
		{"--policy-fd", "3"},
		{"--", "true"},
		{"--policy-fd", "x", "--", "true"},
		{"--policy-fd", "1", "--", "true"},
		{"--policy-fd", "3", "--"},
		{"--bogus", "--", "true"},
	} {
		if _, _, err := ParseArgs(bad); err == nil {
			t.Errorf("ParseArgs(%q) accepted", bad)
		}
	}
}

// TestHelperImports keeps the helper's startup cheap: it dispatches from
// init, so it must not pull Pando's config, logging or database packages (or
// any third-party package) into its dependency set.
func TestHelperImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(path, "/")
			if !strings.Contains(first, ".") || path == "golang.org/x/sys/unix" {
				continue
			}
			t.Errorf("%s imports %s: the helper may only use the standard library and golang.org/x/sys/unix", f, path)
		}
	}
}

func TestRunRejectsBadInvocation(t *testing.T) {
	// Silence the diagnostic.
	stderr := os.Stderr
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = devnull
	defer func() { os.Stderr = stderr; devnull.Close() }()

	if code := Run([]string{"--", "true"}); code != ExitSetupFailed {
		t.Fatalf("Run without --policy-fd = %d, want %d", code, ExitSetupFailed)
	}
}
