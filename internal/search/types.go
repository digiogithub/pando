package search

// DefaultTypes maps type names (as used in --type flag) to glob patterns.
// Ported from ripgrep's crates/ignore/src/default_types.rs
var DefaultTypes = map[string][]string{
	"go":        {"*.go"},
	"js":        {"*.js", "*.mjs", "*.cjs"},
	"ts":        {"*.ts", "*.tsx", "*.mts", "*.cts"},
	"py":        {"*.py", "*.pyi", "*.pyw"},
	"python":    {"*.py", "*.pyi", "*.pyw"},
	"rust":      {"*.rs"},
	"c":         {"*.c", "*.h"},
	"cpp":       {"*.cc", "*.cpp", "*.cxx", "*.hh", "*.hpp", "*.hxx"},
	"java":      {"*.java"},
	"kotlin":    {"*.kt", "*.kts"},
	"swift":     {"*.swift"},
	"cs":        {"*.cs"},
	"csharp":    {"*.cs"},
	"json":      {"*.json"},
	"yaml":      {"*.yml", "*.yaml"},
	"toml":      {"*.toml"},
	"xml":       {"*.xml", "*.xsd", "*.xsl"},
	"html":      {"*.html", "*.htm"},
	"css":       {"*.css", "*.scss", "*.sass", "*.less"},
	"sh":        {"*.sh", "*.bash", "*.zsh", "*.fish"},
	"bash":      {"*.sh", "*.bash"},
	"sql":       {"*.sql"},
	"proto":     {"*.proto"},
	"md":        {"*.md", "*.markdown", "*.mkd"},
	"markdown":  {"*.md", "*.markdown"},
	"rb":        {"*.rb"},
	"ruby":      {"*.rb"},
	"php":       {"*.php"},
	"lua":       {"*.lua"},
	"docker":    {"*Dockerfile*", "*.dockerfile"},
	"makefile":  {"Makefile", "GNUmakefile", "*.mk"},
	"cmake":     {"CMakeLists.txt", "*.cmake"},
	"tf":        {"*.tf", "*.tfvars"},
	"terraform": {"*.tf", "*.tfvars"},
	"gradle":    {"*.gradle", "*.gradle.kts"},
	"nim":       {"*.nim", "*.nims"},
	"zig":       {"*.zig"},
	"dart":      {"*.dart"},
	"elixir":    {"*.ex", "*.exs"},
	"clojure":   {"*.clj", "*.cljc", "*.cljs"},
	"scala":     {"*.scala", "*.sc"},
	"haskell":   {"*.hs", "*.lhs"},
	"r":         {"*.r", "*.R"},
}

// TypeToGlobs returns the glob patterns for a given type name.
// Returns nil, false if the type is not found.
func TypeToGlobs(typeName string) ([]string, bool) {
	globs, ok := DefaultTypes[typeName]
	return globs, ok
}
