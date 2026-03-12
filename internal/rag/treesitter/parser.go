// Package treesitter provides tree-sitter based parsing for code indexing.
package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// Parser is a thread-safe wrapper around tree-sitter parsers
type Parser struct {
	// Cache of parsers per language
	parsers map[Language]*sitter.Parser
	mu      sync.RWMutex
}

// NewParser creates a new Parser instance
func NewParser() *Parser {
	return &Parser{
		parsers: make(map[Language]*sitter.Parser),
	}
}

// getParser returns a parser for the given language, creating one if necessary
func (p *Parser) getParser(lang Language) (*sitter.Parser, error) {
	p.mu.RLock()
	parser, ok := p.parsers[lang]
	p.mu.RUnlock()
	if ok {
		return parser, nil
	}

	// Create new parser
	grammar, ok := GetGrammar(lang)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	parser = sitter.NewParser()
	parser.SetLanguage(grammar)

	p.mu.Lock()
	p.parsers[lang] = parser
	p.mu.Unlock()

	return parser, nil
}

// ParseFile parses a source file and returns the syntax tree
func (p *Parser) ParseFile(ctx context.Context, filePath string) (*sitter.Tree, Language, error) {
	// Detect language from extension
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	lang, ok := GetLanguageByExtension(ext)
	if !ok {
		return nil, "", fmt.Errorf("unsupported file extension: %s", ext)
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, lang, fmt.Errorf("failed to read file: %w", err)
	}

	tree, err := p.Parse(ctx, content, lang)
	return tree, lang, err
}

// Parse parses source code with the specified language
func (p *Parser) Parse(ctx context.Context, sourceCode []byte, lang Language) (*sitter.Tree, error) {
	parser, err := p.getParser(lang)
	if err != nil {
		return nil, err
	}

	tree, err := parser.ParseCtx(ctx, nil, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return tree, nil
}

// ParseWithPreviousTree parses source code with incremental parsing support
func (p *Parser) ParseWithPreviousTree(ctx context.Context, sourceCode []byte, lang Language, oldTree *sitter.Tree) (*sitter.Tree, error) {
	parser, err := p.getParser(lang)
	if err != nil {
		return nil, err
	}

	tree, err := parser.ParseCtx(ctx, oldTree, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return tree, nil
}

// DetectLanguage detects the language from a file path
func DetectLanguage(filePath string) (Language, bool) {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	return GetLanguageByExtension(ext)
}

// IsSupportedFile returns true if the file is a supported source file
func IsSupportedFile(filePath string) bool {
	_, ok := DetectLanguage(filePath)
	return ok
}

// ParseResult contains the result of parsing a source file
type ParserResult struct {
	// The parsed tree
	Tree *sitter.Tree

	// The detected language
	Language Language

	// The source code
	SourceCode []byte

	// File path
	FilePath string
}

// ParseDirectory parses all supported files in a directory (non-recursive)
func (p *Parser) ParseDirectory(ctx context.Context, dirPath string) ([]*ParserResult, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var results []*ParserResult
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		if !IsSupportedFile(filePath) {
			continue
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}

		tree, lang, err := p.ParseFile(ctx, filePath)
		if err != nil {
			continue // Skip files that can't be parsed
		}

		results = append(results, &ParserResult{
			Tree:       tree,
			Language:   lang,
			SourceCode: content,
			FilePath:   filePath,
		})
	}

	return results, nil
}

// Close releases all parser resources
func (p *Parser) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, parser := range p.parsers {
		parser.Close()
	}
	p.parsers = make(map[Language]*sitter.Parser)
}

// NodeIterator provides iteration over nodes in a tree
type NodeIterator struct {
	stack []*sitter.Node
}

// NewNodeIterator creates a new iterator starting from the given node
func NewNodeIterator(root *sitter.Node) *NodeIterator {
	return &NodeIterator{
		stack: []*sitter.Node{root},
	}
}

// Next returns the next node in depth-first order, or nil if done
func (it *NodeIterator) Next() *sitter.Node {
	if len(it.stack) == 0 {
		return nil
	}

	// Pop the next node
	node := it.stack[len(it.stack)-1]
	it.stack = it.stack[:len(it.stack)-1]

	// Push children in reverse order so we process them left-to-right
	childCount := int(node.ChildCount())
	for i := childCount - 1; i >= 0; i-- {
		child := node.Child(i)
		if child != nil {
			it.stack = append(it.stack, child)
		}
	}

	return node
}

// Reset restarts the iterator from a new root
func (it *NodeIterator) Reset(root *sitter.Node) {
	it.stack = []*sitter.Node{root}
}

// IterateNamedChildren iterates over named children of a node
func IterateNamedChildren(node *sitter.Node) []*sitter.Node {
	count := int(node.NamedChildCount())
	children := make([]*sitter.Node, 0, count)
	for i := 0; i < count; i++ {
		child := node.NamedChild(i)
		if child != nil {
			children = append(children, child)
		}
	}
	return children
}

// FindChildByType finds the first child of a specific type
func FindChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

// FindNamedChildByType finds the first named child of a specific type
func FindNamedChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

// FindChildrenByType finds all children of a specific type
func FindChildrenByType(node *sitter.Node, nodeType string) []*sitter.Node {
	var children []*sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			children = append(children, child)
		}
	}
	return children
}

// GetNodeContent returns the source code content for a node
func GetNodeContent(node *sitter.Node, sourceCode []byte) string {
	return node.Content(sourceCode)
}

// GetNodeLocation returns line and byte information for a node
func GetNodeLocation(node *sitter.Node) (startLine, endLine int, startByte, endByte int) {
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	return int(startPoint.Row) + 1, // Convert to 1-based
		int(endPoint.Row) + 1,
		int(node.StartByte()),
		int(node.EndByte())
}
