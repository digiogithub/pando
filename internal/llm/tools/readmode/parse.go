package readmode

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

// Shared, lazily-initialized tree-sitter parser + walker. Parsing is serialized
// per-language inside the parser itself; the walker is stateless after creation.
var (
	sharedParserOnce sync.Once
	sharedParser     *treesitter.Parser
	sharedWalker     *treesitter.ASTWalker
)

func ensureParser() {
	sharedParserOnce.Do(func() {
		sharedParser = treesitter.NewParser()
		// Signatures only; we never need symbol bodies for rendering, which keeps
		// memory bounded for large windows.
		cfg := treesitter.DefaultWalkerConfig()
		cfg.IncludeSourceCode = false
		sharedWalker = treesitter.NewASTWalker(cfg)
	})
}

// ParseSymbols parses the given window content with the language inferred from
// path and returns the extracted symbols. It returns (nil, false) when the file
// is not a supported source language or parsing/extraction fails — callers must
// fall back to the raw window in that case.
func ParseSymbols(ctx context.Context, path string, content []byte) ([]*treesitter.CodeSymbol, treesitter.Language, bool) {
	lang, ok := treesitter.DetectLanguage(path)
	if !ok {
		return nil, "", false
	}
	ensureParser()
	tree, err := sharedParser.Parse(ctx, content, lang)
	if err != nil || tree == nil {
		return nil, lang, false
	}
	defer tree.Close()

	symbols, err := sharedWalker.ExtractSymbols(tree, content, lang, path, "")
	if err != nil {
		return nil, lang, false
	}
	return symbols, lang, true
}

// importPatterns holds per-language regexes that capture import / use targets for
// the `map` outline. Best-effort only — languages without an entry contribute no
// import section.
var importPatterns = map[treesitter.Language][]*regexp.Regexp{
	treesitter.LanguageGo: {
		regexp.MustCompile(`(?m)^\s*(?:[\w.]+\s+)?"([^"]+)"`), // inside import ( ... ) blocks / single import
	},
	treesitter.LanguageTypeScript: {
		regexp.MustCompile(`(?m)^\s*import\b.*?from\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)\brequire\(\s*['"]([^'"]+)['"]\s*\)`),
	},
	treesitter.LanguageJavaScript: {
		regexp.MustCompile(`(?m)^\s*import\b.*?from\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)\brequire\(\s*['"]([^'"]+)['"]\s*\)`),
	},
	treesitter.LanguagePython: {
		regexp.MustCompile(`(?m)^\s*import\s+([\w., ]+)`),
		regexp.MustCompile(`(?m)^\s*from\s+([\w.]+)\s+import\b`),
	},
	treesitter.LanguageRust: {
		regexp.MustCompile(`(?m)^\s*use\s+([\w:{}, *]+);`),
	},
	treesitter.LanguageJava: {
		regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.*]+);`),
	},
	treesitter.LanguagePHP: {
		regexp.MustCompile(`(?m)^\s*use\s+([\w\\, ]+);`),
	},
}

// extractImports returns up to maxImports distinct import targets found in the
// window content for the given language. The Go pattern is scoped to import
// declarations so it does not pick up arbitrary string literals.
func extractImports(content string, lang treesitter.Language) []string {
	pats, ok := importPatterns[lang]
	if !ok {
		return nil
	}
	const maxImports = 40

	scanContent := content
	if lang == treesitter.LanguageGo {
		scanContent = goImportRegion(content)
		if scanContent == "" {
			return nil
		}
	}

	seen := make(map[string]struct{})
	var out []string
	for _, pat := range pats {
		for _, m := range pat.FindAllStringSubmatch(scanContent, -1) {
			if len(m) < 2 {
				continue
			}
			val := strings.TrimSpace(m[1])
			if val == "" {
				continue
			}
			if _, dup := seen[val]; dup {
				continue
			}
			seen[val] = struct{}{}
			out = append(out, val)
			if len(out) >= maxImports {
				return out
			}
		}
	}
	return out
}

// goImportRegion isolates the text of Go import declarations so the quoted-string
// pattern does not match unrelated literals elsewhere in the window.
func goImportRegion(content string) string {
	var b strings.Builder
	lines := strings.Split(content, "\n")
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			if trimmed == ")" {
				inBlock = false
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
		case strings.HasPrefix(trimmed, "import ("):
			inBlock = true
		case strings.HasPrefix(trimmed, "import "):
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
