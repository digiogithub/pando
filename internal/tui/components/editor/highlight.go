package editor

import (
	"bytes"
	"container/list"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

const (
	defaultBlockCacheSize = 128
	defaultLineCacheSize  = 2048
)

// Highlighter provides reusable chroma-based syntax highlighting for TUI code views.
type Highlighter struct {
	theme     tuitheme.Theme
	style     *chroma.Style
	formatter chroma.Formatter

	lexerCache sync.Map // map[string]chroma.Lexer
	blockCache *lruCache
	lineCache  *lruCache
}

type highlightedBlock struct {
	rendered string
	lines    []string
}

// New creates a highlighter using the provided Pando theme.
func New(theme tuitheme.Theme) *Highlighter {
	if theme == nil {
		theme = tuitheme.CurrentTheme()
	}

	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	return &Highlighter{
		theme:      theme,
		style:      themeToChromaStyle(theme),
		formatter:  formatter,
		blockCache: newLRUCache(defaultBlockCacheSize),
		lineCache:  newLRUCache(defaultLineCacheSize),
	}
}

// Highlight applies syntax highlighting to a full source buffer.
func (h *Highlighter) Highlight(source, fileName string) (string, error) {
	block, err := h.renderBlock(source, fileName)
	if err != nil {
		return "", err
	}

	return block.rendered, nil
}

// HighlightSnippet applies syntax highlighting to a code block using the
// declared language when available, falling back to automatic detection.
func (h *Highlighter) HighlightSnippet(source, language string) (string, error) {
	cacheKey := newCacheKey("snippet", strings.ToLower(strings.TrimSpace(language)), source)
	if cached, ok := h.blockCache.Get(cacheKey); ok {
		return cached.(highlightedBlock).rendered, nil
	}

	rendered, err := h.highlightTextWithLexer(source, h.lexerForLanguage(language, source))
	if err != nil {
		return "", err
	}

	block := highlightedBlock{
		rendered: rendered,
		lines:    splitHighlightedLines(rendered),
	}
	h.blockCache.Add(cacheKey, block)

	return block.rendered, nil
}

// HighlightLine applies syntax highlighting to a single line.
// If highlighting fails, the original line is returned unchanged.
func (h *Highlighter) HighlightLine(line, fileName string) string {
	cacheKey := newCacheKey("line", fileName, line)
	if cached, ok := h.lineCache.Get(cacheKey); ok {
		return cached.(string)
	}

	rendered, err := h.highlightText(line, fileName, line)
	if err != nil {
		return line
	}

	h.lineCache.Add(cacheKey, rendered)
	return rendered
}

// HighlightLines returns the highlighted source split into per-line strings.
// If highlighting fails, the original unhighlighted lines are returned.
func (h *Highlighter) HighlightLines(source, fileName string) []string {
	block, err := h.renderBlock(source, fileName)
	if err != nil {
		return strings.Split(source, "\n")
	}

	lines := make([]string, len(block.lines))
	copy(lines, block.lines)
	return lines
}

func (h *Highlighter) renderBlock(source, fileName string) (highlightedBlock, error) {
	cacheKey := newCacheKey("block", fileName, source)
	if cached, ok := h.blockCache.Get(cacheKey); ok {
		return cached.(highlightedBlock), nil
	}

	rendered, err := h.highlightText(source, fileName, source)
	if err != nil {
		return highlightedBlock{}, err
	}

	block := highlightedBlock{
		rendered: rendered,
		lines:    splitHighlightedLines(rendered),
	}
	h.blockCache.Add(cacheKey, block)

	return block, nil
}

func (h *Highlighter) highlightText(source, fileName, analysisSource string) (string, error) {
	lexer := h.lexerFor(fileName, analysisSource)
	return h.highlightTextWithLexer(source, lexer)
}

func (h *Highlighter) highlightTextWithLexer(source string, lexer chroma.Lexer) (string, error) {
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := h.formatter.Format(&buf, h.style, iterator); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (h *Highlighter) lexerForLanguage(language, source string) chroma.Lexer {
	language = strings.ToLower(strings.TrimSpace(language))
	if language != "" {
		cacheKey := "lang:" + language
		if cached, ok := h.lexerCache.Load(cacheKey); ok {
			return cached.(chroma.Lexer)
		}

		lexer := lexers.Get(language)
		if lexer == nil {
			lexer = lexers.Match("file." + language)
		}
		if lexer != nil {
			lexer = chroma.Coalesce(lexer)
			h.lexerCache.Store(cacheKey, lexer)
			return lexer
		}
	}

	return h.lexerFor("", source)
}

func (h *Highlighter) lexerFor(fileName, source string) chroma.Lexer {
	if fileName != "" {
		if cached, ok := h.lexerCache.Load(fileName); ok {
			return cached.(chroma.Lexer)
		}
	}

	lexer := lexers.Match(fileName)
	if lexer == nil {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	lexer = chroma.Coalesce(lexer)

	if fileName != "" {
		h.lexerCache.Store(fileName, lexer)
	}

	return lexer
}

func themeToChromaStyle(theme tuitheme.Theme) *chroma.Style {
	if theme == nil {
		return styles.Fallback
	}

	text := adaptiveColorToString(theme.Text())
	textMuted := adaptiveColorToString(theme.TextMuted())
	textEmphasized := adaptiveColorToString(theme.TextEmphasized())
	background := adaptiveColorToString(theme.Background())
	errorColor := adaptiveColorToString(theme.Error())
	successColor := adaptiveColorToString(theme.Success())
	comment := adaptiveColorToString(theme.SyntaxComment())
	keyword := adaptiveColorToString(theme.SyntaxKeyword())
	function := adaptiveColorToString(theme.SyntaxFunction())
	variable := adaptiveColorToString(theme.SyntaxVariable())
	stringColor := adaptiveColorToString(theme.SyntaxString())
	number := adaptiveColorToString(theme.SyntaxNumber())
	typeColor := adaptiveColorToString(theme.SyntaxType())
	operator := adaptiveColorToString(theme.SyntaxOperator())
	punctuation := adaptiveColorToString(theme.SyntaxPunctuation())

	return chroma.MustNewStyle("pando-editor", chroma.StyleEntries{
		chroma.Background:               fmt.Sprintf("bg:%s %s", background, text),
		chroma.Text:                     text,
		chroma.Other:                    text,
		chroma.Error:                    errorColor,
		chroma.Comment:                  comment,
		chroma.CommentHashbang:          comment,
		chroma.CommentMultiline:         comment,
		chroma.CommentSingle:            comment,
		chroma.CommentSpecial:           comment,
		chroma.CommentPreproc:           keyword,
		chroma.Keyword:                  keyword,
		chroma.KeywordConstant:          keyword,
		chroma.KeywordDeclaration:       keyword,
		chroma.KeywordNamespace:         keyword,
		chroma.KeywordPseudo:            keyword,
		chroma.KeywordReserved:          keyword,
		chroma.KeywordType:              typeColor,
		chroma.Name:                     text,
		chroma.NameAttribute:            variable,
		chroma.NameBuiltin:              typeColor,
		chroma.NameBuiltinPseudo:        variable,
		chroma.NameClass:                typeColor,
		chroma.NameConstant:             variable,
		chroma.NameDecorator:            function,
		chroma.NameEntity:               variable,
		chroma.NameException:            typeColor,
		chroma.NameFunction:             function,
		chroma.NameFunctionMagic:        function,
		chroma.NameLabel:                text,
		chroma.NameNamespace:            typeColor,
		chroma.NameOther:                variable,
		chroma.NameTag:                  keyword,
		chroma.NameVariable:             variable,
		chroma.NameVariableClass:        variable,
		chroma.NameVariableGlobal:       variable,
		chroma.NameVariableInstance:     variable,
		chroma.NameVariableMagic:        variable,
		chroma.Literal:                  stringColor,
		chroma.LiteralDate:              stringColor,
		chroma.LiteralString:            stringColor,
		chroma.LiteralStringAffix:       stringColor,
		chroma.LiteralStringAtom:        stringColor,
		chroma.LiteralStringBacktick:    stringColor,
		chroma.LiteralStringBoolean:     stringColor,
		chroma.LiteralStringChar:        stringColor,
		chroma.LiteralStringDelimiter:   stringColor,
		chroma.LiteralStringDoc:         stringColor,
		chroma.LiteralStringDouble:      stringColor,
		chroma.LiteralStringEscape:      stringColor,
		chroma.LiteralStringHeredoc:     stringColor,
		chroma.LiteralStringInterpol:    stringColor,
		chroma.LiteralStringName:        stringColor,
		chroma.LiteralStringOther:       stringColor,
		chroma.LiteralStringRegex:       stringColor,
		chroma.LiteralStringSingle:      stringColor,
		chroma.LiteralStringSymbol:      stringColor,
		chroma.LiteralNumber:            number,
		chroma.LiteralNumberBin:         number,
		chroma.LiteralNumberFloat:       number,
		chroma.LiteralNumberHex:         number,
		chroma.LiteralNumberInteger:     number,
		chroma.LiteralNumberIntegerLong: number,
		chroma.LiteralNumberOct:         number,
		chroma.Operator:                 operator,
		chroma.OperatorWord:             keyword,
		chroma.Punctuation:              punctuation,
		chroma.Generic:                  text,
		chroma.GenericDeleted:           errorColor,
		chroma.GenericEmph:              "italic",
		chroma.GenericError:             errorColor,
		chroma.GenericHeading:           "bold " + textEmphasized,
		chroma.GenericInserted:          successColor,
		chroma.GenericOutput:            textMuted,
		chroma.GenericPrompt:            text,
		chroma.GenericStrong:            "bold " + textEmphasized,
		chroma.GenericSubheading:        "bold " + textEmphasized,
		chroma.GenericTraceback:         errorColor,
		chroma.TextWhitespace:           textMuted,
	})
}

func adaptiveColorToString(color lipgloss.AdaptiveColor) string {
	if lipgloss.HasDarkBackground() {
		return color.Dark
	}

	return color.Light
}

func splitHighlightedLines(rendered string) []string {
	return strings.Split(rendered, "\n")
}

func newCacheKey(kind, fileName, source string) string {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(kind))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(fileName))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(source))

	return fmt.Sprintf("%s:%016x:%d", kind, hasher.Sum64(), len(source))
}

type lruCache struct {
	mu         sync.Mutex
	maxEntries int
	ll         *list.List
	cache      map[string]*list.Element
}

type lruEntry struct {
	key   string
	value any
}

func newLRUCache(maxEntries int) *lruCache {
	if maxEntries <= 0 {
		maxEntries = 1
	}

	return &lruCache{
		maxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[string]*list.Element, maxEntries),
	}
}

func (c *lruCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.cache[key]
	if !ok {
		return nil, false
	}

	c.ll.MoveToFront(element)
	return element.Value.(*lruEntry).value, true
}

func (c *lruCache) Add(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.cache[key]; ok {
		c.ll.MoveToFront(element)
		element.Value.(*lruEntry).value = value
		return
	}

	element := c.ll.PushFront(&lruEntry{key: key, value: value})
	c.cache[key] = element

	if c.ll.Len() > c.maxEntries {
		c.removeOldest()
	}
}

func (c *lruCache) removeOldest() {
	oldest := c.ll.Back()
	if oldest == nil {
		return
	}

	c.ll.Remove(oldest)
	entry := oldest.Value.(*lruEntry)
	delete(c.cache, entry.key)
}
