// Package outputfilter implements an RTK-style declarative output-compression
// engine. It maps a shell command to a TOML-defined filter and applies an
// 8-stage line-oriented pipeline that strips noise from verbose tool output
// before it reaches the LLM, reducing token consumption.
//
// Design principles ported from RTK (https://github.com/rtk-ai/rtk):
//   - Fail-safe: any error returns the raw output unchanged; the engine never
//     blocks or drops a command's result.
//   - Deterministic: the first filter whose match_command matches wins; filters
//     loaded earlier (project/user overrides) take precedence over built-ins.
//   - Transparent: only the text shown to the LLM is altered, never exit codes.
package outputfilter

import (
	"fmt"
	"regexp"
)

// ReplaceRule is a single chained regex substitution applied per line.
type ReplaceRule struct {
	Pattern     string `toml:"pattern"`
	Replacement string `toml:"replacement"`

	re *regexp.Regexp
}

// MatchRule short-circuits the whole pipeline: if Pattern matches the entire
// output blob (and the optional Unless pattern does not), the filter returns
// Message instead of the output.
type MatchRule struct {
	Pattern string `toml:"pattern"`
	Message string `toml:"message"`
	Unless  string `toml:"unless"`

	re       *regexp.Regexp
	unlessRe *regexp.Regexp
}

// FilterTest is an inline self-test embedded in a filter definition. The test
// runner feeds Input through the filter and asserts the result equals Expected.
type FilterTest struct {
	Name     string `toml:"name"`
	Input    string `toml:"input"`
	Expected string `toml:"expected"`
}

// Filter is a single declarative output filter ([filters.<name>] in TOML).
type Filter struct {
	Description        string        `toml:"description"`
	MatchCommand       string        `toml:"match_command"`
	StripANSI          bool          `toml:"strip_ansi"`
	StripLinesMatching []string      `toml:"strip_lines_matching"`
	KeepLinesMatching  []string      `toml:"keep_lines_matching"`
	Replace            []ReplaceRule `toml:"replace"`
	MatchOutput        []MatchRule   `toml:"match_output"`
	TruncateLinesAt    int           `toml:"truncate_lines_at"`
	HeadLines          int           `toml:"head_lines"`
	TailLines          int           `toml:"tail_lines"`
	MaxLines           int           `toml:"max_lines"`
	OnEmpty            string        `toml:"on_empty"`
	Tests              []FilterTest  `toml:"tests"`

	// name is the TOML section key, set during compilation.
	name string
	// matchRe is the compiled MatchCommand.
	matchRe *regexp.Regexp
	// stripRes / keepRes are the compiled line patterns.
	stripRes []*regexp.Regexp
	keepRes  []*regexp.Regexp
}

// fileSchema is the top-level structure of a filter TOML file.
type fileSchema struct {
	Filters map[string]Filter `toml:"filters"`
}

// Engine holds an ordered list of compiled filters plus the native structured
// parsers. Parsers are consulted before filters; within filters, the first
// match wins (filters added first have higher precedence).
type Engine struct {
	filters []*Filter
	parsers []Parser
}

// New returns an engine with only the built-in native parsers (no filters).
func New() *Engine {
	return &Engine{parsers: defaultParsers()}
}

// Name returns the TOML section key of the filter.
func (f *Filter) Name() string { return f.name }

// compile precompiles every regex in the filter. A filter whose match_command
// is empty or invalid, or that has an invalid line/replace/output pattern, is
// rejected so a bad filter can never break runtime.
func (f *Filter) compile(name string) error {
	if f.MatchCommand == "" {
		return fmt.Errorf("filter %q: match_command is required", name)
	}
	re, err := regexp.Compile(f.MatchCommand)
	if err != nil {
		return fmt.Errorf("filter %q: invalid match_command: %w", name, err)
	}
	if len(f.StripLinesMatching) > 0 && len(f.KeepLinesMatching) > 0 {
		return fmt.Errorf("filter %q: strip_lines_matching and keep_lines_matching are mutually exclusive", name)
	}
	f.name = name
	f.matchRe = re

	f.stripRes = make([]*regexp.Regexp, 0, len(f.StripLinesMatching))
	for _, p := range f.StripLinesMatching {
		r, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("filter %q: invalid strip_lines_matching %q: %w", name, p, err)
		}
		f.stripRes = append(f.stripRes, r)
	}
	f.keepRes = make([]*regexp.Regexp, 0, len(f.KeepLinesMatching))
	for _, p := range f.KeepLinesMatching {
		r, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("filter %q: invalid keep_lines_matching %q: %w", name, p, err)
		}
		f.keepRes = append(f.keepRes, r)
	}
	for i := range f.Replace {
		r, err := regexp.Compile(f.Replace[i].Pattern)
		if err != nil {
			return fmt.Errorf("filter %q: invalid replace pattern %q: %w", name, f.Replace[i].Pattern, err)
		}
		f.Replace[i].re = r
	}
	for i := range f.MatchOutput {
		r, err := regexp.Compile(f.MatchOutput[i].Pattern)
		if err != nil {
			return fmt.Errorf("filter %q: invalid match_output pattern %q: %w", name, f.MatchOutput[i].Pattern, err)
		}
		f.MatchOutput[i].re = r
		if f.MatchOutput[i].Unless != "" {
			ur, err := regexp.Compile(f.MatchOutput[i].Unless)
			if err != nil {
				return fmt.Errorf("filter %q: invalid match_output unless %q: %w", name, f.MatchOutput[i].Unless, err)
			}
			f.MatchOutput[i].unlessRe = ur
		}
	}
	return nil
}

// match reports whether this filter applies to the given command string.
func (f *Filter) match(command string) bool {
	return f.matchRe != nil && f.matchRe.MatchString(command)
}

// Filters returns the engine's compiled filters in precedence order.
func (e *Engine) Filters() []*Filter { return e.filters }

// Apply compresses output for command. It first tries the native structured
// parsers (test runners, linters, compilers) in precedence order; a parser that
// produces a Full or Degraded result wins. Otherwise it falls back to the first
// matching TOML filter and runs its 8-stage pipeline. It returns the compressed
// text, whether anything was applied, and the matched parser/filter name. If
// nothing matches (or a parser only manages passthrough), output is returned
// unchanged with applied=false. Apply never panics and never drops output.
func (e *Engine) Apply(command, output string) (result string, applied bool, name string) {
	if e == nil || command == "" {
		return output, false, ""
	}
	for _, p := range e.parsers {
		if p.Match(command) {
			res, tier := p.parse(output)
			if tier == tierPassthrough {
				return output, false, ""
			}
			return res, true, p.Name()
		}
	}
	for _, f := range e.filters {
		if f.match(command) {
			filtered, ok := f.run(output)
			if !ok {
				return output, false, ""
			}
			return filtered, true, f.name
		}
	}
	return output, false, ""
}
