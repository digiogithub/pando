package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Locked-key coverage of the configuration write path.
//
// Enforcement lives in updateCfgFile, which diffs the file before and after a
// mutation and refuses a write that would change a locked path. That covers
// every mutator that goes through the funnel — and silently fails to cover any
// that does not. A mutator added later which writes state some other way would
// escape the lock with nothing to say so.
//
// This test is what says so. It reads the package's own source, finds every
// exported function whose name says it changes configuration, and requires
// each one to reach either the write funnel or an explicit lock check. A
// function that legitimately does neither is listed in exemptedMutators with
// the reason, so the exemption is a decision on the record rather than an
// omission.

// mutatorNamePattern matches the names the package uses for "this changes
// configuration": Update*, Add*, Delete*, Remove*, Set*, Save*, Rename*
// followed by an upper-case letter, so SetupModelSwitchEnabled (a reader) does
// not match while SetAgeKeysOverride does.
var mutatorNamePattern = regexp.MustCompile(`^(Update|Add|Delete|Remove|Set|Save|Rename)[A-Z]`)

// lockAwareFunctions are the functions that constitute a lock check. Reaching
// any of them, directly or through another function in this package, is what
// makes a mutator covered.
var lockAwareFunctions = map[string]bool{
	"updateCfgFile":         true, // the write funnel; it diffs and refuses
	"ensureNoLockedChange":  true,
	"ErrIfLocked":           true,
	"IsKeyLocked":           true,
	"applyRuntimeOverrides": true, // skips an override of a locked path
}

// exemptedMutators lists the exported mutators that do not consult the lock
// list, each with the reason it must not. Adding a name here is a deliberate
// statement; leaving one out makes the test fail, which is the point.
var exemptedMutators = map[string]string{
	"SetForTests": "test seam: replaces the whole in-memory configuration from a test binary. " +
		"It is not reachable from any user surface and writes nothing, so a lock check " +
		"would only make it impossible to set up a test for locking itself.",

	"SetAgeKeysOverride": "records the --age-keys command-line flag. It runs in cmd/ before " +
		"Load, when no overlay has been consulted and no lock list exists yet. The lock is " +
		"applied where the value is actually used, inside Load.",

	"SetRuntimeOverride": "records a process-local value (--model, --log-file) to be reapplied " +
		"on every load. Nothing is written and nothing takes effect here; applyRuntimeOverrides " +
		"skips an override whose path is locked when the value is merged.",

	"UpdateGlobalProjectName": "writes the user-level project registry (a list of known project " +
		"directories under the XDG config dir), which is a separate file and not part of the " +
		"configuration document. It has no dotted configuration path a lock could name.",
}

func TestConfigMutatorsAreLockAware(t *testing.T) {
	funcs, err := parsePackageFunctions(".")
	if err != nil {
		t.Fatalf("failed to parse the config package: %v", err)
	}

	var uncovered []string
	var staleExemptions []string

	seen := make(map[string]bool)
	for name, fn := range funcs {
		if !ast.IsExported(name) || !mutatorNamePattern.MatchString(name) {
			continue
		}
		seen[name] = true
		if _, exempt := exemptedMutators[name]; exempt {
			continue
		}
		if !reachesLockCheck(name, funcs, map[string]bool{}) {
			uncovered = append(uncovered, name+" ("+fn.pos+")")
		}
	}

	for name := range exemptedMutators {
		if !seen[name] {
			staleExemptions = append(staleExemptions, name)
		}
	}

	sort.Strings(uncovered)
	sort.Strings(staleExemptions)

	if len(uncovered) > 0 {
		t.Errorf("these configuration mutators neither reach the write funnel nor check the lock list:\n  %s\n\n"+
			"Route the mutator through updateCfgFile, add an explicit config.ErrIfLocked check, or add it to "+
			"exemptedMutators in this file with the reason it must not be lock-aware.",
			strings.Join(uncovered, "\n  "))
	}
	if len(staleExemptions) > 0 {
		t.Errorf("exemptedMutators lists functions that no longer exist: %s. Remove them so the list keeps "+
			"describing the code.", strings.Join(staleExemptions, ", "))
	}
}

// parsedFunc is one package-level function: the names it calls inside this
// package, and where it is declared.
type parsedFunc struct {
	calls map[string]bool
	pos   string
}

// parsePackageFunctions reads every non-test Go file in dir and returns the
// package-level functions it declares, keyed by name. Methods are skipped: a
// configuration mutator in this package is a plain function.
func parsePackageFunctions(dir string) (map[string]*parsedFunc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	out := make(map[string]*parsedFunc)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			parsed := &parsedFunc{
				calls: map[string]bool{},
				pos:   fset.Position(fn.Pos()).String(),
			}
			collectCalls(fn.Body, parsed.calls)
			out[fn.Name.Name] = parsed
		}
	}
	return out, nil
}

// collectCalls records the bare identifiers called inside node. Only
// intra-package calls matter, so a selector call (pkg.Func, value.Method) is
// deliberately ignored.
func collectCalls(node ast.Node, into map[string]bool) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			into[ident.Name] = true
		}
		return true
	})
}

// reachesLockCheck reports whether name reaches a lock-aware function through
// intra-package calls. visited guards against recursion.
func reachesLockCheck(name string, funcs map[string]*parsedFunc, visited map[string]bool) bool {
	if lockAwareFunctions[name] {
		return true
	}
	if visited[name] {
		return false
	}
	visited[name] = true

	fn, ok := funcs[name]
	if !ok {
		return false
	}
	for callee := range fn.calls {
		if reachesLockCheck(callee, funcs, visited) {
			return true
		}
	}
	return false
}
