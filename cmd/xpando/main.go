// Command xpando composes a Pando binary from the public core plus one or more
// extension modules.
//
// It is the Pando equivalent of xcaddy: extensions are Go packages linked in at
// build time, so "installing" one means producing a new binary. xpando does
// that mechanically — it generates a throwaway main module whose main.go blank
// imports every requested extension package, resolves it with the normal Go
// toolchain, and builds it.
//
// xpando is internal build tooling. Customers receive a compiled binary and
// never run this command, which is why there is no module registry, lockfile,
// checksum database or signature verification here: the modules it links are
// ones we already control.
//
//	xpando build [core-version]
//	    --with     module[/pkg][@version][=/local/path]   (repeatable)
//	    --replace  module[@version]=replacement           (repeatable)
//	    --tags     enterprise,cuda
//	    --output   ./pando-enterprise
//	    --ldflags  "-X ...=..."
//	    --variant  enterprise
//
// GOOS, GOARCH, GOARM and CGO_ENABLED are read from the environment and passed
// through to the build, so cross-compiling is the same as it is for a plain
// `go build`.
//
// Private modules need no special handling beyond the usual Go setup:
//
//	export GOPRIVATE=github.com/digiogithub/alchemai-agent
//	git config --global url."git@github.com-josedigio:digiogithub/".insteadOf "https://github.com/digiogithub/"
//
// Caveat: the WebUI is embedded in the core module at *its* publish time
// (internal/api/webui/dist), and //go:embed cannot cross module boundaries. A
// composed binary therefore ships the stock frontend unless an extension module
// supplies its own assets.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// coreModule is the public Pando module the composed binary is built from.
const coreModule = "github.com/digiogithub/pando"

// versionPkg is where -X stamps land. Kept in one place so the ldflags this
// command synthesises cannot drift from the package that reads them.
const versionPkg = coreModule + "/internal/version"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "build":
		if err := build(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "xpando build failed: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `xpando composes a Pando binary from the core plus extension modules.

Usage:
  xpando build [core-version] [flags]

Flags:
  --with     module[/pkg][@version][=/local/path]  Extension package to link in (repeatable).
                                                   A "=path" suffix builds against a local checkout.
  --replace  module[@version]=replacement          go.mod replace, without importing (repeatable).
  --tags     tag1,tag2                             Build tags.
  --output   path                                  Output binary (default ./pando).
  --ldflags  flags                                 Extra linker flags, appended after the defaults.
  --variant  name                                  Stamp version.Variant (default "enterprise" when
                                                   --with is used, empty otherwise).
  --keep                                           Keep the generated module instead of deleting it.

Environment:
  GOOS, GOARCH, GOARM, CGO_ENABLED, CC, CXX are passed through to the build.
  XPANDO_SKIP_CLEANUP=1 is equivalent to --keep.
  GOPRIVATE / git insteadOf configure access to private extension modules.

Examples:
  xpando build --with github.com/digiogithub/alchemai-agent/tools --output ./pando-enterprise
  xpando build v0.9.1 --with github.com/digiogithub/alchemai-agent/tools=../alchemai-agent
`)
}

// options is the parsed command line. Parsing is hand-rolled rather than using
// flag because --with must stay ordered and repeatable and may legitimately
// contain "=" in its value.
type options struct {
	coreVersion string
	with        []module
	replaces    []replacement
	tags        string
	output      string
	ldflags     string
	variant     string
	variantSet  bool
	keep        bool
}

// module is one --with entry: an import path to blank import, plus optionally
// the version to request or the local directory to build against.
type module struct {
	importPath string // package to blank import
	modulePath string // module to `go get` (equal to importPath unless a version was given)
	version    string
	localPath  string
}

type replacement struct {
	from string
	to   string
}

func build(args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}

	output, err := filepath.Abs(opts.output)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "xpando-")
	if err != nil {
		return err
	}
	if opts.keep {
		fmt.Fprintf(os.Stderr, "xpando: keeping generated module at %s\n", dir)
	} else {
		defer os.RemoveAll(dir)
	}

	if err := writeMain(dir, opts.with); err != nil {
		return err
	}

	if err := run(dir, "go", "mod", "init", "xpando.build/composed"); err != nil {
		return err
	}

	// Replaces are applied before any resolution so that a local checkout is
	// what gets fetched, not a network version of the same module.
	for _, m := range opts.with {
		if m.localPath == "" {
			continue
		}
		abs, err := filepath.Abs(m.localPath)
		if err != nil {
			return err
		}
		if err := run(dir, "go", "mod", "edit", "-replace", m.modulePath+"="+abs); err != nil {
			return err
		}
	}
	for _, r := range opts.replaces {
		to := r.to
		if strings.HasPrefix(to, ".") || strings.HasPrefix(to, "/") {
			abs, err := filepath.Abs(to)
			if err != nil {
				return err
			}
			to = abs
		}
		if err := run(dir, "go", "mod", "edit", "-replace", r.from+"="+to); err != nil {
			return err
		}
	}

	// The core is resolved first and explicitly. Doing it the other way round
	// lets an extension's own requirement drag the core to a version we did not
	// ask for, which is exactly the surprise a build tool must not produce.
	if replaced(opts, coreModule) {
		// A replaced core is supplied from disk; asking the proxy for it would
		// fail or, worse, pin a version the replace then ignores.
		if opts.coreVersion != "" {
			return fmt.Errorf("core version %q was given but %s is replaced locally", opts.coreVersion, coreModule)
		}
	} else {
		core := coreModule
		if opts.coreVersion != "" {
			core += "@" + opts.coreVersion
		}
		if err := run(dir, "go", "get", core); err != nil {
			return fmt.Errorf("resolve core module: %w", err)
		}
	}

	for _, m := range opts.with {
		if m.localPath != "" {
			continue // supplied by the replace above
		}
		target := m.modulePath
		if m.version != "" {
			target += "@" + m.version
		}
		if err := run(dir, "go", "get", target); err != nil {
			return fmt.Errorf("resolve %s: %w", m.modulePath, err)
		}
	}

	if err := run(dir, "go", "mod", "tidy"); err != nil {
		return err
	}

	buildArgs := []string{"build", "-o", output}
	if opts.tags != "" {
		buildArgs = append(buildArgs, "-tags", opts.tags)
	}
	buildArgs = append(buildArgs, "-ldflags", ldflags(opts))
	buildArgs = append(buildArgs, ".")

	if err := run(dir, "go", buildArgs...); err != nil {
		return err
	}

	fmt.Printf("xpando: built %s (%s/%s)\n", output, goenv("GOOS", runtime.GOOS), goenv("GOARCH", runtime.GOARCH))
	return nil
}

// ldflags builds the linker flags: the variant stamp first, the caller's own
// flags last so they can override anything decided here.
func ldflags(opts options) string {
	var parts []string
	if v := opts.variantValue(); v != "" {
		parts = append(parts, fmt.Sprintf("-X %s.Variant=%s", versionPkg, v))
	}
	// A composed binary is not built by `go install`, so nothing sets the
	// version for it and it would report "unknown". When the core version was
	// named on the command line, that is the answer.
	if opts.coreVersion != "" && !strings.Contains(opts.ldflags, versionPkg+".Version=") {
		parts = append(parts, fmt.Sprintf("-X %s.Version=%s", versionPkg, opts.coreVersion))
	}
	if opts.ldflags != "" {
		parts = append(parts, opts.ldflags)
	}
	return strings.Join(parts, " ")
}

// variantValue resolves the variant stamp. A binary composed with extra modules
// is not a stock build and should not report itself as one, so --with implies
// "enterprise"; an explicit --variant (including an empty one) always wins.
func (o options) variantValue() string {
	if o.variantSet {
		return o.variant
	}
	if len(o.with) > 0 {
		return "enterprise"
	}
	return ""
}

// writeMain generates the composed main package. It is one call to cmd.Main so
// the composed binary behaves exactly like the repository's own main.go,
// panic handler included.
func writeMain(dir string, modules []module) error {
	var b strings.Builder
	b.WriteString("// Code generated by xpando. DO NOT EDIT.\n\npackage main\n\nimport (\n")
	fmt.Fprintf(&b, "\tpandocmd %q\n", coreModule+"/cmd")
	if len(modules) > 0 {
		b.WriteString("\n")
		for _, m := range modules {
			fmt.Fprintf(&b, "\t_ %q\n", m.importPath)
		}
	}
	b.WriteString(")\n\nfunc main() {\n\tpandocmd.Main()\n}\n")

	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644)
}

func parseArgs(args []string) (options, error) {
	opts := options{output: "./pando"}
	if _, skip := os.LookupEnv("XPANDO_SKIP_CLEANUP"); skip {
		opts.keep = true
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// A bare argument is the core version, as in `xpando build v0.9.1`.
		if !strings.HasPrefix(arg, "-") {
			if opts.coreVersion != "" {
				return opts, fmt.Errorf("unexpected argument %q: the core version is already set to %q", arg, opts.coreVersion)
			}
			opts.coreVersion = arg
			continue
		}

		name, inlineValue, hasInline := strings.Cut(arg, "=")
		name = strings.TrimLeft(name, "-")

		// --keep takes no value, so it is handled before the value lookup.
		if name == "keep" {
			opts.keep = true
			continue
		}

		value := inlineValue
		if !hasInline {
			if i+1 >= len(args) {
				return opts, fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			value = args[i]
		}

		switch name {
		case "with":
			m, err := parseModule(value)
			if err != nil {
				return opts, err
			}
			opts.with = append(opts.with, m)
		case "replace":
			from, to, ok := strings.Cut(value, "=")
			if !ok || from == "" || to == "" {
				return opts, fmt.Errorf("--replace wants module=replacement, got %q", value)
			}
			opts.replaces = append(opts.replaces, replacement{from: from, to: to})
		case "tags":
			opts.tags = value
		case "output":
			opts.output = value
		case "ldflags":
			opts.ldflags = value
		case "variant":
			opts.variant = value
			opts.variantSet = true
		default:
			return opts, fmt.Errorf("unknown flag --%s", name)
		}
	}

	if len(opts.with) == 0 && len(opts.replaces) == 0 {
		// Composing nothing is almost certainly a mistake, and the result would
		// be a plain core build the Makefile produces faster.
		return opts, errors.New("nothing to compose: pass at least one --with module")
	}
	return opts, nil
}

// parseModule reads one --with value. The grammar is
// path[@version][=localPath], where path is an *import* path that may sit below
// the module root: "example.com/mod/tools@v1.2.0" imports .../mod/tools while
// resolving the module that provides it. Resolution is left to `go get`, which
// walks up the path itself, so the module and import path are the same string
// here unless a version pins it.
func parseModule(value string) (module, error) {
	if value == "" {
		return module{}, errors.New("--with needs a module path")
	}

	m := module{}
	rest := value
	if before, after, ok := strings.Cut(rest, "="); ok {
		if after == "" {
			return module{}, fmt.Errorf("--with %q: local path is empty", value)
		}
		rest = before
		m.localPath = after
	}
	if before, after, ok := strings.Cut(rest, "@"); ok {
		if after == "" {
			return module{}, fmt.Errorf("--with %q: version is empty", value)
		}
		rest = before
		m.version = after
	}
	if rest == "" {
		return module{}, fmt.Errorf("--with %q: module path is empty", value)
	}
	m.importPath = rest
	m.modulePath = rest

	if m.localPath != "" {
		if m.version != "" {
			return module{}, fmt.Errorf("--with %q: a local path and a version are mutually exclusive", value)
		}
		// A replace directive targets a *module*, while --with names an import
		// path that is often a package below the module root. The root cannot
		// be derived from the import path, so read it from the checkout's own
		// go.mod instead of replacing something that does not exist.
		root, err := moduleRoot(m.localPath)
		if err != nil {
			return module{}, fmt.Errorf("--with %q: %w", value, err)
		}
		if !withinModule(m.importPath, root) {
			return module{}, fmt.Errorf("--with %q: import path is not inside module %s", value, root)
		}
		m.modulePath = root
	}
	return m, nil
}

// moduleRoot reads the module path declared by the go.mod of a local checkout.
func moduleRoot(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if path, ok := strings.CutPrefix(line, "module"); ok {
			path = strings.TrimSpace(path)
			if path != "" {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%s declares no module path", filepath.Join(dir, "go.mod"))
}

// withinModule reports whether an import path belongs to a module.
func withinModule(importPath, module string) bool {
	return importPath == module || strings.HasPrefix(importPath, module+"/")
}

// replaced reports whether a module is satisfied by a replace directive rather
// than by the module proxy.
func replaced(opts options, path string) bool {
	for _, r := range opts.replaces {
		if from, _, _ := strings.Cut(r.from, "@"); from == path {
			return true
		}
	}
	for _, m := range opts.with {
		if m.localPath != "" && m.modulePath == path {
			return true
		}
	}
	return false
}

func goenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // build chatter is progress, not output
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
