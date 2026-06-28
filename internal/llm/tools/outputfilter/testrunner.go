package outputfilter

import "strings"

// FilterTestResult is the outcome of running one inline [[filters.<name>.tests]]
// case through its owning filter's pipeline. Trailing newlines are ignored when
// comparing Got against Expected (the pipeline normalises them).
type FilterTestResult struct {
	Filter   string // owning filter name
	Test     string // the test's `name` (may be empty)
	Passed   bool
	Got      string // actual pipeline output
	Expected string // declared expected output
	// Err is non-empty when the filter reported a hard failure on the input
	// (its pipeline short-circuited to raw); the case is counted as failed.
	Err string
}

// LoadFileFilters builds an engine from a single TOML filter file WITHOUT the
// embedded defaults, so authors can test exactly the filters declared in their
// file. It is fail-safe like Load: a malformed filter contributes an error but
// never aborts the rest. Native parsers are not seeded — only declarative
// filters carry inline tests.
func LoadFileFilters(path string, data []byte) (*Engine, error) {
	filters, err := parseFile(data, path)
	return &Engine{filters: filters}, err
}

// RunInlineTests runs every filter's inline [[tests]] in declaration order and
// returns one FilterTestResult per case. It never panics; a filter that reports
// failure on its input yields a failed result with Err set.
func (e *Engine) RunInlineTests() []FilterTestResult {
	if e == nil {
		return nil
	}
	var results []FilterTestResult
	for _, f := range e.filters {
		for _, tc := range f.Tests {
			res := FilterTestResult{Filter: f.Name(), Test: tc.Name, Expected: tc.Expected}
			got, ok := f.run(tc.Input)
			res.Got = got
			if !ok {
				res.Err = "filter reported failure on input"
				results = append(results, res)
				continue
			}
			res.Passed = strings.TrimRight(got, "\n") == strings.TrimRight(tc.Expected, "\n")
			results = append(results, res)
		}
	}
	return results
}
