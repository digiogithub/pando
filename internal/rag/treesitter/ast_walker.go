// Package treesitter provides AST walking and symbol extraction utilities.
package treesitter

import (
	"time"

	"github.com/google/uuid"
	sitter "github.com/madeindigio/go-tree-sitter"
)

// SymbolExtractor defines the interface for language-specific symbol extraction
type SymbolExtractor interface {
	// Language returns the language this extractor handles
	Language() Language

	// ExtractSymbols extracts all symbols from a parsed tree
	ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error)

	// GetSymbolTypes returns the types of symbols this extractor can find
	GetSymbolTypes() []SymbolType
}

// ASTWalker provides utilities for walking the AST and extracting symbols
type ASTWalker struct {
	// Registry of extractors by language
	extractors map[Language]SymbolExtractor

	// Configuration
	config WalkerConfig
}

// WalkerConfig holds configuration for the AST walker
type WalkerConfig struct {
	// IncludeSourceCode determines if source code is included in symbols
	IncludeSourceCode bool

	// MaxSymbolSize is the maximum size of source code to include (in bytes)
	MaxSymbolSize int

	// ExtractDocStrings determines if documentation comments are extracted
	ExtractDocStrings bool
}

// DefaultWalkerConfig returns sensible default configuration
func DefaultWalkerConfig() WalkerConfig {
	return WalkerConfig{
		IncludeSourceCode: true,
		MaxSymbolSize:     50000, // 50KB max per symbol
		ExtractDocStrings: true,
	}
}

// NewASTWalker creates a new AST walker with the given configuration
func NewASTWalker(config WalkerConfig) *ASTWalker {
	walker := &ASTWalker{
		extractors: make(map[Language]SymbolExtractor),
		config:     config,
	}

	// Register default extractors
	walker.RegisterExtractor(NewGoExtractor(config))
	walker.RegisterExtractor(NewTypeScriptExtractor(config))
	walker.RegisterExtractor(NewJavaScriptExtractor(config))
	walker.RegisterExtractor(NewPHPExtractor(config))
	walker.RegisterExtractor(NewRustExtractor(config))
	walker.RegisterExtractor(NewJavaExtractor(config))
	walker.RegisterExtractor(NewKotlinExtractor(config))
	walker.RegisterExtractor(NewSwiftExtractor(config))
	walker.RegisterExtractor(NewCExtractor(config))
	walker.RegisterExtractor(NewPythonExtractor(config))
	walker.RegisterExtractor(NewLuaExtractor(config))
	walker.RegisterExtractor(NewSvelteExtractor(config))
	walker.RegisterExtractor(NewTOMLExtractor(config))
	walker.RegisterExtractor(NewMarkdownExtractor(config))
	walker.RegisterExtractor(NewVueExtractor(config))

	return walker
}

// RegisterExtractor adds a new extractor for a language
func (w *ASTWalker) RegisterExtractor(extractor SymbolExtractor) {
	w.extractors[extractor.Language()] = extractor
}

// GetExtractor returns the extractor for a language
func (w *ASTWalker) GetExtractor(lang Language) (SymbolExtractor, bool) {
	ext, ok := w.extractors[lang]
	return ext, ok
}

// ExtractSymbols extracts symbols from a parsed tree using the appropriate extractor
func (w *ASTWalker) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, lang Language, filePath string, projectID string) ([]*CodeSymbol, error) {
	extractor, ok := w.GetExtractor(lang)
	if !ok {
		// Fall back to generic extractor
		extractor = NewGenericExtractor(w.config)
	}

	return extractor.ExtractSymbols(tree, sourceCode, filePath, projectID)
}

// BaseExtractor provides common functionality for all extractors
type BaseExtractor struct {
	config   WalkerConfig
	language Language
}

// NewBaseExtractor creates a new base extractor
func NewBaseExtractor(lang Language, config WalkerConfig) BaseExtractor {
	return BaseExtractor{
		config:   config,
		language: lang,
	}
}

// Language returns the language this extractor handles
func (b *BaseExtractor) Language() Language {
	return b.language
}

// CreateSymbol creates a new CodeSymbol from a node
func (b *BaseExtractor) CreateSymbol(
	node *sitter.Node,
	sourceCode []byte,
	symbolType SymbolType,
	name string,
	namePath string,
	filePath string,
	projectID string,
	parentID *string,
) *CodeSymbol {
	startLine, endLine, startByte, endByte := GetNodeLocation(node)

	symbol := &CodeSymbol{
		ID:         uuid.New().String(),
		ProjectID:  projectID,
		FilePath:   filePath,
		Language:   b.language,
		SymbolType: symbolType,
		Name:       name,
		NamePath:   namePath,
		StartLine:  startLine,
		EndLine:    endLine,
		StartByte:  startByte,
		EndByte:    endByte,
		ParentID:   parentID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Include source code if configured
	if b.config.IncludeSourceCode {
		code := GetNodeContent(node, sourceCode)
		if len(code) <= b.config.MaxSymbolSize {
			symbol.SourceCode = code
		}
	}

	return symbol
}

// ExtractDocString extracts documentation comment before a node
func (b *BaseExtractor) ExtractDocString(node *sitter.Node, sourceCode []byte) string {
	if !b.config.ExtractDocStrings {
		return ""
	}

	// Look for a comment sibling before this node
	parent := node.Parent()
	if parent == nil {
		return ""
	}

	// Find index of this node in parent
	idx := -1
	for i := 0; i < int(parent.NamedChildCount()); i++ {
		if parent.NamedChild(i) == node {
			idx = i
			break
		}
	}

	if idx <= 0 {
		return ""
	}

	// Check previous sibling for comment
	prevSibling := parent.NamedChild(idx - 1)
	if prevSibling == nil {
		return ""
	}

	nodeType := prevSibling.Type()
	if nodeType == "comment" || nodeType == "block_comment" || nodeType == "line_comment" ||
		nodeType == "documentation_comment" || nodeType == "doc_comment" {
		return GetNodeContent(prevSibling, sourceCode)
	}

	return ""
}

// BuildNamePath creates a hierarchical path for a symbol
func (b *BaseExtractor) BuildNamePath(parentPath string, name string) string {
	if parentPath == "" {
		return "/" + name
	}
	return parentPath + "/" + name
}

// GenericExtractor is a fallback extractor that works with any language
type GenericExtractor struct {
	BaseExtractor
}

// NewGenericExtractor creates a new generic extractor
func NewGenericExtractor(config WalkerConfig) *GenericExtractor {
	return &GenericExtractor{
		BaseExtractor: NewBaseExtractor("unknown", config),
	}
}

// ExtractSymbols extracts symbols using generic node type detection
func (g *GenericExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Common node types across languages
	symbolNodes := map[string]SymbolType{
		"function_definition":     SymbolTypeFunction,
		"function_declaration":    SymbolTypeFunction,
		"method_definition":       SymbolTypeMethod,
		"method_declaration":      SymbolTypeMethod,
		"class_definition":        SymbolTypeClass,
		"class_declaration":       SymbolTypeClass,
		"struct_definition":       SymbolTypeStruct,
		"struct_declaration":      SymbolTypeStruct,
		"interface_definition":    SymbolTypeInterface,
		"interface_declaration":   SymbolTypeInterface,
		"enum_definition":         SymbolTypeEnum,
		"enum_declaration":        SymbolTypeEnum,
		"type_definition":         SymbolTypeTypeAlias,
		"type_declaration":        SymbolTypeTypeAlias,
		"const_declaration":       SymbolTypeConstant,
		"variable_declaration":    SymbolTypeVariable,
		"function_item":           SymbolTypeFunction,
		"impl_item":               SymbolTypeClass,
		"trait_definition":        SymbolTypeTrait,
		"module_definition":       SymbolTypeModule,
		"namespace_definition":    SymbolTypeNamespace,
		"package_declaration":     SymbolTypePackage,
		"constructor_declaration": SymbolTypeConstructor,
	}

	// Walk tree looking for symbol nodes
	iter := NewNodeIterator(root)
	for node := iter.Next(); node != nil; node = iter.Next() {
		nodeType := node.Type()
		symbolType, ok := symbolNodes[nodeType]
		if !ok {
			continue
		}

		// Try to find name in common ways
		name := findNodeName(node, sourceCode)
		if name == "" {
			continue
		}

		symbol := g.CreateSymbol(
			node,
			sourceCode,
			symbolType,
			name,
			"/"+name,
			filePath,
			projectID,
			nil,
		)
		symbol.DocString = g.ExtractDocString(node, sourceCode)
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// GetSymbolTypes returns the symbol types the generic extractor can find
func (g *GenericExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeClass,
		SymbolTypeStruct,
		SymbolTypeInterface,
		SymbolTypeEnum,
		SymbolTypeTypeAlias,
		SymbolTypeConstant,
		SymbolTypeVariable,
	}
}

// findNodeName attempts to find the name of a node using common patterns
func findNodeName(node *sitter.Node, sourceCode []byte) string {
	// Try common name field patterns
	nameFields := []string{"name", "identifier", "declarator"}

	for _, field := range nameFields {
		child := node.ChildByFieldName(field)
		if child != nil {
			// Handle nested identifiers
			if child.Type() == "pointer_declarator" || child.Type() == "function_declarator" {
				nested := child.ChildByFieldName("declarator")
				if nested != nil {
					return GetNodeContent(nested, sourceCode)
				}
			}
			return GetNodeContent(child, sourceCode)
		}
	}

	// Try first identifier child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && (child.Type() == "identifier" || child.Type() == "type_identifier") {
			return GetNodeContent(child, sourceCode)
		}
	}

	return ""
}
