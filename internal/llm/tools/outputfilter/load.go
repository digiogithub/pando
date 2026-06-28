package outputfilter

import (
	_ "embed"
	"fmt"
	"os"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

//go:embed defaults.toml
var defaultsTOML []byte

// parseFile unmarshals a filter TOML blob and compiles every filter. Filters
// that fail to compile are skipped (with a collected error) so one bad filter
// never breaks the whole set. Returns the compiled filters sorted by name for
// deterministic precedence within a single source.
func parseFile(data []byte, source string) ([]*Filter, error) {
	var schema fileSchema
	if err := toml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	names := make([]string, 0, len(schema.Filters))
	for name := range schema.Filters {
		names = append(names, name)
	}
	sort.Strings(names)

	filters := make([]*Filter, 0, len(names))
	var firstErr error
	for _, name := range names {
		f := schema.Filters[name]
		if err := f.compile(name); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", source, err)
			}
			continue
		}
		filters = append(filters, &f)
	}
	return filters, firstErr
}

// LoadDefaults returns an engine populated only with the embedded built-in
// filters. It never returns nil; a parse error yields an empty engine.
func LoadDefaults() (*Engine, error) {
	filters, err := parseFile(defaultsTOML, "builtin defaults")
	e := &Engine{filters: filters, parsers: defaultParsers()}
	return e, err
}

// Load builds an engine from the embedded defaults plus any extra TOML files in
// paths. Earlier paths take precedence over later ones, and all paths take
// precedence over the embedded defaults (first match wins). Missing files are
// skipped silently; malformed files contribute an error but never abort the
// build, preserving the fail-safe contract.
func Load(paths ...string) (*Engine, error) {
	var ordered []*Filter
	var firstErr error

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = fmt.Errorf("read %s: %w", p, err)
			}
			continue
		}
		filters, perr := parseFile(data, p)
		if perr != nil && firstErr == nil {
			firstErr = perr
		}
		ordered = append(ordered, filters...)
	}

	defaults, derr := parseFile(defaultsTOML, "builtin defaults")
	if derr != nil && firstErr == nil {
		firstErr = derr
	}
	ordered = append(ordered, defaults...)

	return &Engine{filters: ordered, parsers: defaultParsers()}, firstErr
}
