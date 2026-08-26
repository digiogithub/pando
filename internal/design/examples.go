package design

import (
	"embed"
	"path"
	"sort"
	"strings"
)

// The bundled examples are written style guides, not token files. They are
// shipped so a project can start from a coherent, opinionated system instead of
// the neutral default, and so the text extractor has something real to be
// tested against. They are Pando-authored prose (locked decision 12): we speak
// the format, we do not vendor anyone else's content.
//
//go:embed examples/*.md
var exampleSystems embed.FS

// ExampleSystemNames lists the bundled style guides, sorted for a stable
// listing in every surface that offers them.
func ExampleSystemNames() []string {
	entries, err := exampleSystems.ReadDir("examples")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

// ExampleSystem returns the prose of a bundled style guide.
func ExampleSystem(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return "", false
	}
	raw, err := exampleSystems.ReadFile(path.Join("examples", name+".md"))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// ExampleSystemTitle returns the first heading of a bundled guide, which is
// what a picker should show next to its name.
func ExampleSystemTitle(name string) string {
	body, ok := ExampleSystem(name)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return name
}
