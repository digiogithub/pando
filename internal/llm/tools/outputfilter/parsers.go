package outputfilter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// parseTier mirrors RTK's OutputParser degradation ladder. A parser tries the
// richest representation first and falls back gracefully so it can never break
// a command's result.
type parseTier int

const (
	// tierFull is a complete structured parse (e.g. NDJSON / JSON decoded into
	// a failure-focused summary).
	tierFull parseTier = iota
	// tierDegraded is a best-effort regex extraction used when the structured
	// form is unavailable or malformed.
	tierDegraded
	// tierPassthrough means the parser could not improve the output; the raw
	// text is returned unchanged.
	tierPassthrough
)

// Parser is a native, code-driven output compressor for commands whose output
// is structured (test runners, linters, compilers) and not well served by the
// line-oriented TOML pipeline. Parsers take precedence over TOML filters: when
// a parser matches a command, its result is used (unless it degrades all the
// way to passthrough).
//
// Each parser implements RTK's 3-tier degradation internally: Full (structured)
// → Degraded (regex) → Passthrough (raw). This keeps the contract fail-safe —
// parse never returns an error, only a tier.
type Parser interface {
	// Name identifies the parser (used in debug/metadata).
	Name() string
	// Match reports whether the parser applies to the command.
	Match(command string) bool
	// parse compresses output and reports which tier produced the result.
	parse(output string) (string, parseTier)
}

// defaultParsers returns the built-in native parsers in precedence order.
func defaultParsers() []Parser {
	return []Parser{
		&goTestJSONParser{},
		&golangciLintParser{},
		&tscParser{},
	}
}

// degradedGrep keeps only lines matching keep, capped to maxLines (0 = no cap).
// It returns the joined lines and whether anything matched. Used as the shared
// Degraded tier for parsers whose structured form is unavailable.
func degradedGrep(output string, keep *regexp.Regexp, maxLines int) (string, bool) {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		if keep.MatchString(ln) {
			kept = append(kept, ln)
		}
	}
	if len(kept) == 0 {
		return "", false
	}
	truncated := false
	if maxLines > 0 && len(kept) > maxLines {
		kept = kept[:maxLines]
		truncated = true
	}
	res := strings.Join(kept, "\n")
	if truncated {
		res += "\n… (more lines omitted)"
	}
	return res, true
}

// ---------------------------------------------------------------------------
// go test -json
// ---------------------------------------------------------------------------

// goTestEvent is one NDJSON record emitted by `go test -json`.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

type goTestJSONParser struct{}

var (
	goTestJSONCmd   = regexp.MustCompile(`(^|\s)go\s+test\b`)
	goTestJSONFlag  = regexp.MustCompile(`(^|\s)-+json\b`)
	goTestNoiseLine = regexp.MustCompile(`^\s*(=== (RUN|CONT|PAUSE|NAME)\b|--- (PASS|SKIP):|PASS$|ok\s|FAIL$|FAIL\s)`)
	goTestDegraded  = regexp.MustCompile(`(?i)(--- FAIL|^\s*FAIL\b|panic:|\.go:\d+:|^#\s)`)
)

func (p *goTestJSONParser) Name() string { return "go-test-json" }

func (p *goTestJSONParser) Match(command string) bool {
	return goTestJSONCmd.MatchString(command) && goTestJSONFlag.MatchString(command)
}

func (p *goTestJSONParser) parse(output string) (string, parseTier) {
	type failure struct {
		pkg, test string
		out       []string
	}
	var (
		failed       []failure
		outputs      = map[string][]string{}
		pkgErrors    = map[string][]string{}
		pkgSet       = map[string]struct{}{}
		failedPkgs   = map[string]struct{}{}
		nonJSON      []string
		jsonCount    int
		passN, skipN int
	)

	for _, raw := range strings.Split(output, "\n") {
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		if !strings.HasPrefix(trim, "{") {
			nonJSON = append(nonJSON, raw)
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal([]byte(trim), &ev); err != nil {
			nonJSON = append(nonJSON, raw)
			continue
		}
		jsonCount++
		if ev.Package != "" {
			pkgSet[ev.Package] = struct{}{}
		}
		key := ev.Package + "\t" + ev.Test
		switch ev.Action {
		case "output":
			if ev.Test != "" {
				outputs[key] = append(outputs[key], ev.Output)
			} else if line := strings.TrimRight(ev.Output, "\n"); line != "" && !goTestNoiseLine.MatchString(line) {
				pkgErrors[ev.Package] = append(pkgErrors[ev.Package], line)
			}
		case "pass":
			if ev.Test != "" {
				passN++
			}
		case "skip":
			if ev.Test != "" {
				skipN++
			}
		case "fail":
			if ev.Package != "" {
				failedPkgs[ev.Package] = struct{}{}
			}
			if ev.Test != "" {
				failed = append(failed, failure{pkg: ev.Package, test: ev.Test, out: cleanGoTestOutput(outputs[key])})
			}
		}
	}

	// No structured records at all: degrade to a regex grep, then passthrough.
	if jsonCount == 0 {
		if res, ok := degradedGrep(output, goTestDegraded, 200); ok {
			return res, tierDegraded
		}
		return output, tierPassthrough
	}

	var b strings.Builder
	for _, f := range failed {
		fmt.Fprintf(&b, "FAIL %s · %s\n", f.pkg, f.test)
		for _, ln := range f.out {
			b.WriteString("    ")
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	// Package-level errors (build/compile failures captured outside any test).
	pkgErrNames := sortedKeys(pkgErrors)
	for _, pkg := range pkgErrNames {
		fmt.Fprintf(&b, "BUILD %s\n", pkg)
		for _, ln := range pkgErrors[pkg] {
			b.WriteString("    ")
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	// Non-JSON leftovers (rare top-level build errors) are surfaced too.
	for _, ln := range nonJSON {
		b.WriteString(ln)
		b.WriteString("\n")
	}

	b.WriteString(goTestSummary(passN, len(failed), skipN, len(pkgSet), len(failedPkgs)))
	return b.String(), tierFull
}

// cleanGoTestOutput flattens accumulated Output strings into individual lines
// and drops go test's structural noise, keeping only failure-relevant text.
func cleanGoTestOutput(chunks []string) []string {
	var lines []string
	for _, c := range chunks {
		for _, ln := range strings.Split(strings.TrimRight(c, "\n"), "\n") {
			if ln == "" || goTestNoiseLine.MatchString(ln) {
				continue
			}
			lines = append(lines, ln)
		}
	}
	return lines
}

func goTestSummary(pass, fail, skip, pkgs, failPkgs int) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%d passed", pass))
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", fail))
	}
	if skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skip))
	}
	pkgNote := fmt.Sprintf("%d packages", pkgs)
	if failPkgs > 0 {
		pkgNote += fmt.Sprintf(", %d failed", failPkgs)
	}
	tail := ""
	if fail == 0 {
		tail = " — all pass"
	}
	return fmt.Sprintf("SUMMARY: %s (%s)%s", strings.Join(parts, ", "), pkgNote, tail)
}

// ---------------------------------------------------------------------------
// golangci-lint
// ---------------------------------------------------------------------------

type golangciLintParser struct{}

type golangciReport struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Pos        struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
			Column   int    `json:"Column"`
		} `json:"Pos"`
	} `json:"Issues"`
}

var (
	golangciCmd      = regexp.MustCompile(`(^|\s)golangci-lint\b`)
	golangciDegraded = regexp.MustCompile(`:\d+:\d+:`)
)

const golangciMaxIssues = 60

func (p *golangciLintParser) Name() string { return "golangci-lint" }

func (p *golangciLintParser) Match(command string) bool {
	return golangciCmd.MatchString(command)
}

func (p *golangciLintParser) parse(output string) (string, parseTier) {
	trim := strings.TrimSpace(output)
	if strings.HasPrefix(trim, "{") {
		var rep golangciReport
		if err := json.Unmarshal([]byte(trim), &rep); err == nil {
			return p.renderJSON(rep), tierFull
		}
	}
	if res, ok := degradedGrep(output, golangciDegraded, 200); ok {
		return res, tierDegraded
	}
	return output, tierPassthrough
}

func (p *golangciLintParser) renderJSON(rep golangciReport) string {
	if len(rep.Issues) == 0 {
		return "SUMMARY: 0 issues — clean"
	}
	byLinter := map[string]int{}
	files := map[string]struct{}{}
	var b strings.Builder
	shown := 0
	for _, is := range rep.Issues {
		byLinter[is.FromLinter]++
		files[is.Pos.Filename] = struct{}{}
		if shown < golangciMaxIssues {
			fmt.Fprintf(&b, "%s:%d:%d [%s] %s\n",
				is.Pos.Filename, is.Pos.Line, is.Pos.Column, is.FromLinter, is.Text)
			shown++
		}
	}
	if len(rep.Issues) > shown {
		fmt.Fprintf(&b, "… (%d more issues omitted)\n", len(rep.Issues)-shown)
	}
	linters := sortedKeys(byLinter)
	parts := make([]string, 0, len(linters))
	for _, l := range linters {
		parts = append(parts, fmt.Sprintf("%s: %d", l, byLinter[l]))
	}
	fmt.Fprintf(&b, "SUMMARY: %d issues across %d files (%s)",
		len(rep.Issues), len(files), strings.Join(parts, ", "))
	return b.String()
}

// ---------------------------------------------------------------------------
// tsc (TypeScript compiler)
// ---------------------------------------------------------------------------

type tscParser struct{}

var (
	tscCmd      = regexp.MustCompile(`(^|\s|/)tsc\b`)
	tscDiag     = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\):\s+error\s+(TS\d+):\s+(.*)$`)
	tscDegraded = regexp.MustCompile(`error TS\d+`)
)

const tscMaxErrors = 60

func (p *tscParser) Name() string { return "tsc" }

func (p *tscParser) Match(command string) bool {
	return tscCmd.MatchString(command)
}

func (p *tscParser) parse(output string) (string, parseTier) {
	type diag struct{ file, line, col, code, msg string }
	var diags []diag
	files := map[string]struct{}{}
	for _, ln := range strings.Split(output, "\n") {
		m := tscDiag.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		diags = append(diags, diag{file: m[1], line: m[2], col: m[3], code: m[4], msg: m[5]})
		files[m[1]] = struct{}{}
	}
	if len(diags) == 0 {
		if res, ok := degradedGrep(output, tscDegraded, 200); ok {
			return res, tierDegraded
		}
		return output, tierPassthrough
	}

	var b strings.Builder
	shown := 0
	for _, d := range diags {
		if shown >= tscMaxErrors {
			break
		}
		fmt.Fprintf(&b, "%s:%s:%s [%s] %s\n", d.file, d.line, d.col, d.code, d.msg)
		shown++
	}
	if len(diags) > shown {
		fmt.Fprintf(&b, "… (%d more errors omitted)\n", len(diags)-shown)
	}
	fmt.Fprintf(&b, "SUMMARY: %d errors in %d files", len(diags), len(files))
	return b.String(), tierFull
}

// sortedKeys returns the map keys sorted for deterministic output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
