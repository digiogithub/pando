// Package treesitter provides tree-sitter based parsing and AST extraction for code indexing.
package treesitter

import (
	"time"
)

// SymbolType represents the type of a code symbol
type SymbolType string

const (
	SymbolTypeClass       SymbolType = "class"
	SymbolTypeStruct      SymbolType = "struct"
	SymbolTypeInterface   SymbolType = "interface"
	SymbolTypeTrait       SymbolType = "trait"
	SymbolTypeMethod      SymbolType = "method"
	SymbolTypeFunction    SymbolType = "function"
	SymbolTypeConstructor SymbolType = "constructor"
	SymbolTypeProperty    SymbolType = "property"
	SymbolTypeField       SymbolType = "field"
	SymbolTypeVariable    SymbolType = "variable"
	SymbolTypeConstant    SymbolType = "constant"
	SymbolTypeEnum        SymbolType = "enum"
	SymbolTypeEnumMember  SymbolType = "enum_member"
	SymbolTypeTypeAlias   SymbolType = "type_alias"
	SymbolTypeNamespace   SymbolType = "namespace"
	SymbolTypeModule      SymbolType = "module"
	SymbolTypePackage     SymbolType = "package"
)

// Language represents a supported programming language
type Language string

const (
	LanguageGo         Language = "go"
	LanguageTypeScript Language = "typescript"
	LanguageJavaScript Language = "javascript"
	LanguagePHP        Language = "php"
	LanguageRust       Language = "rust"
	LanguageJava       Language = "java"
	LanguageKotlin     Language = "kotlin"
	LanguageSwift      Language = "swift"
	LanguageObjectiveC Language = "objc"
	LanguageC          Language = "c"
	LanguageCPP        Language = "cpp"
	LanguagePython     Language = "python"
	LanguageRuby       Language = "ruby"
	LanguageCSharp     Language = "csharp"
	LanguageLua        Language = "lua"
	LanguageMarkdown   Language = "markdown"
	LanguageSvelte     Language = "svelte"
	LanguageTOML       Language = "toml"
	LanguageVue        Language = "vue"
)

// CodeSymbol represents a parsed code symbol from source code
type CodeSymbol struct {
	// Unique identifier for the symbol
	ID string `json:"id"`

	// Project this symbol belongs to
	ProjectID string `json:"project_id"`

	// Relative file path within the project
	FilePath string `json:"file_path"`

	// Programming language
	Language Language `json:"language"`

	// Type of symbol (class, method, function, etc.)
	SymbolType SymbolType `json:"symbol_type"`

	// Name of the symbol
	Name string `json:"name"`

	// Hierarchical path within the file (e.g., "MyClass/myMethod")
	NamePath string `json:"name_path"`

	// Location in source file
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`

	// Source code content
	SourceCode string `json:"source_code,omitempty"`

	// Signature (for methods/functions)
	Signature string `json:"signature,omitempty"`

	// Documentation string
	DocString string `json:"doc_string,omitempty"`

	// Vector embedding (populated later during indexing)
	Embedding []float32 `json:"embedding,omitempty"`

	// Parent symbol ID (for nested symbols like methods in classes)
	ParentID *string `json:"parent_id,omitempty"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Children symbols (populated when fetching with depth)
	Children []*CodeSymbol `json:"children,omitempty"`
}

// CodeFile represents an indexed source file
type CodeFile struct {
	// Project identifier
	ProjectID string `json:"project_id"`

	// Relative file path
	FilePath string `json:"file_path"`

	// Detected language
	Language Language `json:"language"`

	// SHA-256 hash for change detection
	FileHash string `json:"file_hash"`

	// Number of symbols in this file
	SymbolsCount int `json:"symbols_count"`

	// When this file was indexed
	IndexedAt time.Time `json:"indexed_at"`
}

// CodeProject represents an indexed code project
type CodeProject struct {
	// Unique project identifier
	ProjectID string `json:"project_id"`

	// Human-readable name
	Name string `json:"name"`

	// Root path on disk
	RootPath string `json:"root_path"`

	// Statistics by language
	LanguageStats map[Language]int `json:"language_stats"`

	// Last time the project was indexed
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`

	// Current indexing status
	IndexingStatus IndexingStatus `json:"indexing_status"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IndexingStatus represents the status of an indexing job
type IndexingStatus string

const (
	IndexingStatusPending    IndexingStatus = "pending"
	IndexingStatusInProgress IndexingStatus = "in_progress"
	IndexingStatusCompleted  IndexingStatus = "completed"
	IndexingStatusFailed     IndexingStatus = "failed"
	IndexingStatusCancelled  IndexingStatus = "cancelled"
)

// IndexingJob represents an async indexing job
type IndexingJob struct {
	// Job identifier
	ID string `json:"id"`

	// Project being indexed
	ProjectID string `json:"project_id"`

	// Path to the project
	ProjectPath string `json:"project_path"`

	// Current status
	Status IndexingStatus `json:"status"`

	// Progress percentage (0-100)
	Progress float64 `json:"progress"`

	// File counts
	FilesTotal   int `json:"files_total"`
	FilesIndexed int `json:"files_indexed"`

	// Timing
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Error message if failed
	Error *string `json:"error,omitempty"`
}

// ParseResult represents the result of parsing a source file
type ParseResult struct {
	// The file that was parsed
	FilePath string `json:"file_path"`

	// Detected language
	Language Language `json:"language"`

	// Extracted symbols
	Symbols []*CodeSymbol `json:"symbols"`

	// Parse errors (if any)
	Errors []ParseError `json:"errors,omitempty"`
}

// ParseError represents a parsing error
type ParseError struct {
	// Error message
	Message string `json:"message"`

	// Location
	Line   int `json:"line"`
	Column int `json:"column"`
}

// SymbolQuery represents search criteria for finding symbols
type SymbolQuery struct {
	// Project to search in
	ProjectID string `json:"project_id"`

	// Name pattern (supports wildcards)
	NamePattern string `json:"name_pattern,omitempty"`

	// Name path pattern
	NamePathPattern string `json:"name_path_pattern,omitempty"`

	// Restrict to specific file or directory
	RelativePath string `json:"relative_path,omitempty"`

	// Include children up to this depth
	Depth int `json:"depth,omitempty"`

	// Include source code in results
	IncludeBody bool `json:"include_body,omitempty"`

	// Filter by symbol types
	IncludeTypes []SymbolType `json:"include_types,omitempty"`
	ExcludeTypes []SymbolType `json:"exclude_types,omitempty"`

	// Filter by languages
	Languages []Language `json:"languages,omitempty"`

	// Enable substring matching
	SubstringMatch bool `json:"substring_match,omitempty"`

	// Maximum results
	Limit int `json:"limit,omitempty"`
}

// SemanticSearchQuery represents a semantic search query
type SemanticSearchQuery struct {
	// Project to search in
	ProjectID string `json:"project_id"`

	// Natural language query
	Query string `json:"query"`

	// Maximum results
	Limit int `json:"limit,omitempty"`

	// Filter by languages
	Languages []Language `json:"languages,omitempty"`

	// Filter by symbol types
	SymbolTypes []SymbolType `json:"symbol_types,omitempty"`
}

// SemanticSearchResult represents a result from semantic search
type SemanticSearchResult struct {
	// The matched symbol
	Symbol *CodeSymbol `json:"symbol"`

	// Similarity score (0-1)
	Score float64 `json:"score"`
}

// Visibility represents symbol visibility/access level
type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityPrivate   Visibility = "private"
	VisibilityProtected Visibility = "protected"
	VisibilityInternal  Visibility = "internal"
	VisibilityPackage   Visibility = "package"
)

// SymbolModifiers holds modifiers applicable to symbols
type SymbolModifiers struct {
	Visibility Visibility `json:"visibility,omitempty"`
	Static     bool       `json:"static,omitempty"`
	Abstract   bool       `json:"abstract,omitempty"`
	Final      bool       `json:"final,omitempty"`
	Async      bool       `json:"async,omitempty"`
	Const      bool       `json:"const,omitempty"`
	Readonly   bool       `json:"readonly,omitempty"`
}
