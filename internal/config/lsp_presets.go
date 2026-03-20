package config

// LSPPreset defines a well-known LSP server configuration template.
type LSPPreset struct {
	// Name is the identifier used as map key in Config.LSP.
	Name        string
	Description string
	Config      LSPConfig
}

// LSPPresets returns the list of built-in LSP server presets.
// These represent the most commonly used language servers; users can
// override command/args by editing their project config.
func LSPPresets() []LSPPreset {
	return []LSPPreset{
		{
			Name:        "gopls",
			Description: "Go language server (golang.org/x/tools/gopls)",
			Config: LSPConfig{
				Command:   "gopls",
				Args:      []string{},
				Languages: []string{".go"},
			},
		},
		{
			Name:        "rust-analyzer",
			Description: "Rust language server (rust-analyzer)",
			Config: LSPConfig{
				Command:   "rust-analyzer",
				Args:      []string{},
				Languages: []string{".rs"},
			},
		},
		{
			Name:        "typescript-language-server",
			Description: "TypeScript/JavaScript language server (typescript-language-server)",
			Config: LSPConfig{
				Command:   "typescript-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
			},
		},
		{
			Name:        "pyright",
			Description: "Python static type checker and language server (pyright)",
			Config: LSPConfig{
				Command:   "pyright-langserver",
				Args:      []string{"--stdio"},
				Languages: []string{".py", ".pyi"},
			},
		},
		{
			Name:        "pylsp",
			Description: "Python language server (python-lsp-server)",
			Config: LSPConfig{
				Command:   "pylsp",
				Args:      []string{},
				Languages: []string{".py", ".pyi"},
			},
		},
		{
			Name:        "clangd",
			Description: "C/C++/Objective-C language server (clangd)",
			Config: LSPConfig{
				Command:   "clangd",
				Args:      []string{},
				Languages: []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".h", ".hh", ".hpp", ".m", ".mm"},
			},
		},
		{
			Name:        "lua-language-server",
			Description: "Lua language server (lua-language-server)",
			Config: LSPConfig{
				Command:   "lua-language-server",
				Args:      []string{},
				Languages: []string{".lua"},
			},
		},
		{
			Name:        "bash-language-server",
			Description: "Bash/Shell language server (bash-language-server)",
			Config: LSPConfig{
				Command:   "bash-language-server",
				Args:      []string{"start"},
				Languages: []string{".sh", ".bash", ".zsh", ".ksh"},
			},
		},
		{
			Name:        "yaml-language-server",
			Description: "YAML language server (yaml-language-server)",
			Config: LSPConfig{
				Command:   "yaml-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".yaml", ".yml"},
			},
		},
		{
			Name:        "json-language-server",
			Description: "JSON language server (vscode-json-languageserver)",
			Config: LSPConfig{
				Command:   "vscode-json-languageserver",
				Args:      []string{"--stdio"},
				Languages: []string{".json", ".jsonc"},
			},
		},
		{
			Name:        "html-language-server",
			Description: "HTML language server (vscode-html-languageserver)",
			Config: LSPConfig{
				Command:   "vscode-html-languageserver",
				Args:      []string{"--stdio"},
				Languages: []string{".html", ".htm"},
			},
		},
		{
			Name:        "css-language-server",
			Description: "CSS/SCSS/Less language server (vscode-css-languageserver)",
			Config: LSPConfig{
				Command:   "vscode-css-languageserver",
				Args:      []string{"--stdio"},
				Languages: []string{".css", ".scss", ".sass", ".less"},
			},
		},
		{
			Name:        "marksman",
			Description: "Markdown language server (marksman)",
			Config: LSPConfig{
				Command:   "marksman",
				Args:      []string{"server"},
				Languages: []string{".md", ".markdown"},
			},
		},
		{
			Name:        "jdtls",
			Description: "Java language server (Eclipse JDT Language Server)",
			Config: LSPConfig{
				Command:   "jdtls",
				Args:      []string{},
				Languages: []string{".java"},
			},
		},
		{
			Name:        "solargraph",
			Description: "Ruby language server (solargraph)",
			Config: LSPConfig{
				Command:   "solargraph",
				Args:      []string{"stdio"},
				Languages: []string{".rb", ".rake"},
			},
		},
		{
			Name:        "zls",
			Description: "Zig language server (zls)",
			Config: LSPConfig{
				Command:   "zls",
				Args:      []string{},
				Languages: []string{".zig"},
			},
		},
		{
			Name:        "kotlin-language-server",
			Description: "Kotlin language server (kotlin-language-server)",
			Config: LSPConfig{
				Command:   "kotlin-language-server",
				Args:      []string{},
				Languages: []string{".kt", ".kts"},
			},
		},
		{
			Name:        "intelephense",
			Description: "PHP language server (intelephense)",
			Config: LSPConfig{
				Command:   "intelephense",
				Args:      []string{"--stdio"},
				Languages: []string{".php"},
			},
		},
		{
			Name:        "omnisharp",
			Description: "C# language server (OmniSharp)",
			Config: LSPConfig{
				Command:   "omnisharp",
				Args:      []string{"--languageserver"},
				Languages: []string{".cs"},
			},
		},
		{
			Name:        "dartls",
			Description: "Dart language server (dart analysis server)",
			Config: LSPConfig{
				Command:   "dart",
				Args:      []string{"language-server", "--protocol=lsp"},
				Languages: []string{".dart"},
			},
		},
		{
			Name:        "elixir-ls",
			Description: "Elixir language server (ElixirLS)",
			Config: LSPConfig{
				Command:   "elixir-ls",
				Args:      []string{},
				Languages: []string{".ex", ".exs"},
			},
		},
	}
}

// LSPPresetByName returns the preset with the given name, or false if not found.
func LSPPresetByName(name string) (LSPPreset, bool) {
	for _, p := range LSPPresets() {
		if p.Name == name {
			return p, true
		}
	}
	return LSPPreset{}, false
}
