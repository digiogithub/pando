// Package treesitter provides language mappings and grammar access for tree-sitter parsing.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
	"github.com/madeindigio/go-tree-sitter/bash"
	"github.com/madeindigio/go-tree-sitter/c"
	"github.com/madeindigio/go-tree-sitter/cpp"
	"github.com/madeindigio/go-tree-sitter/csharp"
	"github.com/madeindigio/go-tree-sitter/css"
	"github.com/madeindigio/go-tree-sitter/golang"
	"github.com/madeindigio/go-tree-sitter/html"
	"github.com/madeindigio/go-tree-sitter/java"
	"github.com/madeindigio/go-tree-sitter/javascript"
	"github.com/madeindigio/go-tree-sitter/kotlin"
	"github.com/madeindigio/go-tree-sitter/lua"
	"github.com/madeindigio/go-tree-sitter/markdown"
	"github.com/madeindigio/go-tree-sitter/php"
	"github.com/madeindigio/go-tree-sitter/python"
	"github.com/madeindigio/go-tree-sitter/ruby"
	"github.com/madeindigio/go-tree-sitter/rust"
	"github.com/madeindigio/go-tree-sitter/scala"
	"github.com/madeindigio/go-tree-sitter/svelte"
	"github.com/madeindigio/go-tree-sitter/swift"
	"github.com/madeindigio/go-tree-sitter/toml"
	"github.com/madeindigio/go-tree-sitter/typescript/tsx"
	"github.com/madeindigio/go-tree-sitter/typescript/typescript"
	"github.com/madeindigio/go-tree-sitter/vue2"
	"github.com/madeindigio/go-tree-sitter/yaml"
)

// LanguageInfo holds metadata about a supported language
type LanguageInfo struct {
	// Language identifier
	Language Language

	// Human-readable name
	Name string

	// File extensions (without dot)
	Extensions []string

	// Tree-sitter language getter
	Grammar func() *sitter.Language
}

// supportedLanguages maps Language enum to LanguageInfo
var supportedLanguages = map[Language]LanguageInfo{
	LanguageGo: {
		Language:   LanguageGo,
		Name:       "Go",
		Extensions: []string{"go"},
		Grammar:    golang.GetLanguage,
	},
	LanguageTypeScript: {
		Language:   LanguageTypeScript,
		Name:       "TypeScript",
		Extensions: []string{"ts", "mts", "cts"},
		Grammar:    typescript.GetLanguage,
	},
	LanguageJavaScript: {
		Language:   LanguageJavaScript,
		Name:       "JavaScript",
		Extensions: []string{"js", "mjs", "cjs", "jsx"},
		Grammar:    javascript.GetLanguage,
	},
	LanguagePHP: {
		Language:   LanguagePHP,
		Name:       "PHP",
		Extensions: []string{"php", "phtml", "php3", "php4", "php5", "phps"},
		Grammar:    php.GetLanguage,
	},
	LanguageLua: {
		Language:   LanguageLua,
		Name:       "Lua",
		Extensions: []string{"lua"},
		Grammar:    lua.GetLanguage,
	},
	LanguageMarkdown: {
		Language:   LanguageMarkdown,
		Name:       "Markdown",
		Extensions: []string{"md", "markdown"},
		Grammar:    markdown.GetLanguage,
	},
	LanguageSvelte: {
		Language:   LanguageSvelte,
		Name:       "Svelte",
		Extensions: []string{"svelte"},
		Grammar:    svelte.GetLanguage,
	},
	LanguageTOML: {
		Language:   LanguageTOML,
		Name:       "TOML",
		Extensions: []string{"toml"},
		Grammar:    toml.GetLanguage,
	},
	LanguageVue: {
		Language:   LanguageVue,
		Name:       "Vue",
		Extensions: []string{"vue"},
		Grammar:    vue2.GetLanguage,
	},
	LanguageRust: {
		Language:   LanguageRust,
		Name:       "Rust",
		Extensions: []string{"rs"},
		Grammar:    rust.GetLanguage,
	},
	LanguageJava: {
		Language:   LanguageJava,
		Name:       "Java",
		Extensions: []string{"java"},
		Grammar:    java.GetLanguage,
	},
	LanguageKotlin: {
		Language:   LanguageKotlin,
		Name:       "Kotlin",
		Extensions: []string{"kt", "kts"},
		Grammar:    kotlin.GetLanguage,
	},
	LanguageSwift: {
		Language:   LanguageSwift,
		Name:       "Swift",
		Extensions: []string{"swift"},
		Grammar:    swift.GetLanguage,
	},
	LanguageObjectiveC: {
		Language:   LanguageObjectiveC,
		Name:       "Objective-C",
		Extensions: []string{"m", "mm", "h"},
		Grammar:    c.GetLanguage, // Use C grammar as fallback for Objective-C
	},
	LanguageC: {
		Language:   LanguageC,
		Name:       "C",
		Extensions: []string{"c"},
		Grammar:    c.GetLanguage,
	},
	LanguageCPP: {
		Language:   LanguageCPP,
		Name:       "C++",
		Extensions: []string{"cpp", "cc", "cxx", "hpp", "hxx", "hh"},
		Grammar:    cpp.GetLanguage,
	},
	LanguagePython: {
		Language:   LanguagePython,
		Name:       "Python",
		Extensions: []string{"py", "pyw", "pyi"},
		Grammar:    python.GetLanguage,
	},
	LanguageRuby: {
		Language:   LanguageRuby,
		Name:       "Ruby",
		Extensions: []string{"rb", "rake", "gemspec"},
		Grammar:    ruby.GetLanguage,
	},
	LanguageCSharp: {
		Language:   LanguageCSharp,
		Name:       "C#",
		Extensions: []string{"cs"},
		Grammar:    csharp.GetLanguage,
	},
}

// Additional languages that can be enabled
var additionalLanguages = map[Language]LanguageInfo{
	"tsx": {
		Language:   "tsx",
		Name:       "TSX",
		Extensions: []string{"tsx"},
		Grammar:    tsx.GetLanguage,
	},
	"scala": {
		Language:   "scala",
		Name:       "Scala",
		Extensions: []string{"scala", "sc"},
		Grammar:    scala.GetLanguage,
	},
	"bash": {
		Language:   "bash",
		Name:       "Bash",
		Extensions: []string{"sh", "bash", "zsh"},
		Grammar:    bash.GetLanguage,
	},
	"yaml": {
		Language:   "yaml",
		Name:       "YAML",
		Extensions: []string{"yml", "yaml"},
		Grammar:    yaml.GetLanguage,
	},
	"html": {
		Language:   "html",
		Name:       "HTML",
		Extensions: []string{"html", "htm"},
		Grammar:    html.GetLanguage,
	},
	"css": {
		Language:   "css",
		Name:       "CSS",
		Extensions: []string{"css"},
		Grammar:    css.GetLanguage,
	},
}

// extensionToLanguage maps file extensions to Language
var extensionToLanguage map[string]Language

func init() {
	extensionToLanguage = make(map[string]Language)

	// Build extension map from supported languages
	for lang, info := range supportedLanguages {
		for _, ext := range info.Extensions {
			extensionToLanguage[ext] = lang
		}
	}

	// Add additional languages to extension map
	for lang, info := range additionalLanguages {
		for _, ext := range info.Extensions {
			extensionToLanguage[ext] = lang
		}
	}
}

// GetLanguageByExtension returns the Language for a file extension (without dot)
func GetLanguageByExtension(ext string) (Language, bool) {
	lang, ok := extensionToLanguage[ext]
	return lang, ok
}

// GetLanguageInfo returns the LanguageInfo for a Language
func GetLanguageInfo(lang Language) (LanguageInfo, bool) {
	info, ok := supportedLanguages[lang]
	if ok {
		return info, true
	}
	info, ok = additionalLanguages[lang]
	return info, ok
}

// GetGrammar returns the tree-sitter grammar for a Language
func GetGrammar(lang Language) (*sitter.Language, bool) {
	info, ok := GetLanguageInfo(lang)
	if !ok {
		return nil, false
	}
	return info.Grammar(), true
}

// IsLanguageSupported returns true if the language is supported
func IsLanguageSupported(lang Language) bool {
	_, ok := GetLanguageInfo(lang)
	return ok
}

// GetSupportedLanguages returns all supported language identifiers
func GetSupportedLanguages() []Language {
	languages := make([]Language, 0, len(supportedLanguages)+len(additionalLanguages))
	for lang := range supportedLanguages {
		languages = append(languages, lang)
	}
	for lang := range additionalLanguages {
		languages = append(languages, lang)
	}
	return languages
}

// GetSupportedExtensions returns all supported file extensions
func GetSupportedExtensions() []string {
	extensions := make([]string, 0, len(extensionToLanguage))
	for ext := range extensionToLanguage {
		extensions = append(extensions, ext)
	}
	return extensions
}
