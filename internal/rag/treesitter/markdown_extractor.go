// Package treesitter provides Markdown language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// MarkdownExtractor extracts symbols from Markdown source code
type MarkdownExtractor struct {
	BaseExtractor
}

// NewMarkdownExtractor creates a new Markdown extractor
func NewMarkdownExtractor(config WalkerConfig) *MarkdownExtractor {
	return &MarkdownExtractor{
		BaseExtractor: NewBaseExtractor(LanguageMarkdown, config),
	}
}

// GetSymbolTypes returns the types of symbols the Markdown extractor can find
func (m *MarkdownExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeNamespace, // Using namespace for headings/sections
	}
}

// ExtractSymbols extracts all symbols from Markdown source code
func (m *MarkdownExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Walk through the document extracting headings
	symbols = m.extractHeadings(root, sourceCode, filePath, projectID, "", nil)

	return symbols, nil
}

// extractHeadings extracts headings from markdown
func (m *MarkdownExtractor) extractHeadings(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol
	var currentSymbol *CodeSymbol
	var currentPath string
	var currentID *string

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		nodeType := child.Type()

		// Look for various heading types
		switch nodeType {
		case "section", "atx_heading", "setext_heading", "heading":
			if symbol := m.extractHeading(child, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
				symbols = append(symbols, symbol)
				currentSymbol = symbol
				currentPath = symbol.NamePath
				currentID = &symbol.ID
			}

		default:
			// For nested structures, recurse
			if currentSymbol != nil {
				childSymbols := m.extractHeadings(child, sourceCode, filePath, projectID, currentPath, currentID)
				if len(childSymbols) > 0 {
					currentSymbol.Children = append(currentSymbol.Children, childSymbols...)
					symbols = append(symbols, childSymbols...)
				}
			}
		}
	}

	return symbols
}

// extractHeading extracts a heading node
func (m *MarkdownExtractor) extractHeading(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Try to find the heading content
	var name string

	// Look for heading_content or inline content
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		if child.Type() == "heading_content" || child.Type() == "inline" {
			name = GetNodeContent(child, sourceCode)
			break
		}
	}

	// If we couldn't find named content, use the whole node
	if name == "" {
		content := GetNodeContent(node, sourceCode)
		// Clean up markdown heading markers
		if len(content) > 0 && content[0] == '#' {
			// Remove leading # symbols and whitespace
			for i, c := range content {
				if c != '#' && c != ' ' {
					name = content[i:]
					break
				}
			}
		} else {
			name = content
		}
	}

	if name == "" {
		return nil
	}

	// Trim whitespace
	name = trimString(name)
	if name == "" {
		return nil
	}

	namePath := m.BuildNamePath(parentPath, name)

	symbol := m.CreateSymbol(node, sourceCode, SymbolTypeNamespace, name, namePath, filePath, projectID, parentID)

	return symbol
}

// trimString removes leading and trailing whitespace
func trimString(s string) string {
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// Trim trailing whitespace
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
