// Package treesitter provides Markdown language symbol extraction.
package treesitter

import (
	"strings"

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

// extractHeadings extracts headings from markdown. It handles both the
// section-wrapped grammar (document → section → heading + body + nested
// sections) and flat layouts (heading and content as siblings), emitting one
// granular symbol per heading instead of collapsing a whole file into a single
// blob.
func (m *MarkdownExtractor) extractHeadings(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol
	var currentSymbol *CodeSymbol
	currentPath := parentPath
	currentID := parentID

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Type() {
		case "section":
			sectionSymbols := m.extractSection(child, sourceCode, filePath, projectID, parentPath, parentID)
			symbols = append(symbols, sectionSymbols...)
			if len(sectionSymbols) > 0 {
				currentSymbol = sectionSymbols[0]
				currentPath = currentSymbol.NamePath
				currentID = &currentSymbol.ID
			}

		case "atx_heading", "setext_heading", "heading":
			if symbol := m.extractHeading(child, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
				symbols = append(symbols, symbol)
				currentSymbol = symbol
				currentPath = symbol.NamePath
				currentID = &symbol.ID
			}

		default:
			// For nested structures, recurse under the current heading.
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

// extractSection creates a symbol for a section's heading and recurses into any
// nested sections, so each markdown heading becomes its own granular symbol.
// The heading child itself is not re-processed, avoiding duplicate symbols.
func (m *MarkdownExtractor) extractSection(section *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	sym := m.extractHeading(section, sourceCode, filePath, projectID, parentPath, parentID)
	if sym == nil {
		// Section without a recognizable heading: still descend for nested ones.
		return m.extractHeadings(section, sourceCode, filePath, projectID, parentPath, parentID)
	}

	symbols := []*CodeSymbol{sym}
	for i := 0; i < int(section.NamedChildCount()); i++ {
		child := section.NamedChild(i)
		if child == nil || child.Type() != "section" {
			continue
		}
		nested := m.extractSection(child, sourceCode, filePath, projectID, sym.NamePath, &sym.ID)
		sym.Children = append(sym.Children, nested...)
		symbols = append(symbols, nested...)
	}
	return symbols
}

// extractHeading extracts a heading node.
//
// The node passed in can be either a heading node directly (atx_heading,
// setext_heading) or a "section" node that wraps a heading plus its body. We
// keep the (possibly section-wide) node for location and source_code extraction
// — useful for FTS and snippets — but the symbol Name must only ever be the
// heading text, never the section body. Otherwise markdown symbols carry
// multi-thousand-character names that bloat every search result.
func (m *MarkdownExtractor) extractHeading(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	headingNode := findHeadingNode(node)

	// Try to find the heading content within the heading node.
	var name string
	for i := 0; i < int(headingNode.NamedChildCount()); i++ {
		child := headingNode.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Type() == "heading_content" || child.Type() == "inline" {
			name = GetNodeContent(child, sourceCode)
			break
		}
	}

	// Fall back to the heading node's own content (not the section body).
	if name == "" {
		name = GetNodeContent(headingNode, sourceCode)
	}

	// Defensive: only ever keep the first non-empty line and strip markdown
	// heading markers, so a section body can never leak into the name.
	name = headingTextOnly(name)
	if name == "" {
		return nil
	}

	namePath := m.BuildNamePath(parentPath, name)

	symbol := m.CreateSymbol(node, sourceCode, SymbolTypeNamespace, name, namePath, filePath, projectID, parentID)

	return symbol
}

// findHeadingNode returns the heading node itself if node is already a heading,
// otherwise it descends into a "section" wrapper to locate the first heading
// child. Falls back to the original node when no heading child is found.
func findHeadingNode(node *sitter.Node) *sitter.Node {
	switch node.Type() {
	case "atx_heading", "setext_heading", "heading":
		return node
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "atx_heading", "setext_heading", "heading":
			return child
		}
	}
	return node
}

// headingTextOnly reduces raw heading content to a clean single-line title:
// it keeps only the first non-empty line and strips leading '#' markers.
func headingTextOnly(content string) string {
	content = trimString(content)
	if content == "" {
		return ""
	}
	// Keep only the first line to guard against body leakage.
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		content = content[:idx]
	}
	// Strip leading markdown heading markers (#) and surrounding whitespace.
	content = strings.TrimLeft(content, "#")
	return trimString(content)
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
