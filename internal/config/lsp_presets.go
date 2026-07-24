package config

// LSPInstallStrategy identifies how a language server binary is obtained when
// it is not already on PATH.
type LSPInstallStrategy string

const (
	// LSPInstallNone means no provisioning path is known for the server.
	LSPInstallNone LSPInstallStrategy = ""
	// LSPInstallNpm means the server is an npm package Pando can install with
	// bun or npm into its own staging directory.
	LSPInstallNpm LSPInstallStrategy = "npm"
	// LSPInstallManual means the server ships through a language toolchain or a
	// platform package manager. Pando cannot install it and only reports the
	// command the user should run.
	LSPInstallManual LSPInstallStrategy = "manual"
)

// LSPInstall describes how to provision a language server binary.
type LSPInstall struct {
	// Strategy selects the provisioning path.
	Strategy LSPInstallStrategy
	// Package is the npm package spec (LSPInstallNpm only).
	Package string
	// Bin is the executable the package exposes; it is frequently different
	// from the package name (pyright exposes pyright-langserver).
	Bin string
	// ExtraPackages are peer dependencies the package manager will not pull on
	// its own, e.g. "typescript" for typescript-language-server.
	ExtraPackages []string
	// Hint is the command a user can run to install the server themselves.
	Hint string
	// URL points at the upstream installation documentation.
	URL string
}

// LSPPreset defines a well-known LSP server configuration template.
type LSPPreset struct {
	// Name is the identifier used as map key in Config.LSP.
	Name        string
	Description string
	Config      LSPConfig
	// Install describes how Pando can provision the server when its command is
	// not found on PATH.
	Install LSPInstall
	// OptIn keeps the preset out of automatic activation until the user
	// declares it under [LSP.<name>]. It is used for servers that only make
	// sense with project configuration (linters, formatters) or that would
	// otherwise compete with a general-purpose server for the same files.
	OptIn bool
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "go install golang.org/x/tools/gopls@latest",
				URL:      "https://pkg.go.dev/golang.org/x/tools/gopls",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "rustup component add rust-analyzer",
				URL:      "https://rust-analyzer.github.io/manual.html#installation",
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
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "typescript-language-server",
				Bin:      "typescript-language-server",
				// The server refuses to start without a TypeScript install and
				// declares it as a peer dependency, which is not installed
				// automatically.
				ExtraPackages: []string{"typescript"},
				Hint:          "npm install -g typescript-language-server typescript",
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
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "pyright",
				Bin:      "pyright-langserver",
				Hint:     "npm install -g pyright",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "pipx install python-lsp-server",
				URL:      "https://github.com/python-lsp/python-lsp-server",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "apt install clangd (Linux) or brew install llvm (macOS)",
				URL:      "https://clangd.llvm.org/installation",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install lua-language-server, or download a release",
				URL:      "https://luals.github.io/#install",
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
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "bash-language-server",
				Bin:      "bash-language-server",
				Hint:     "npm install -g bash-language-server",
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
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "yaml-language-server",
				Bin:      "yaml-language-server",
				Hint:     "npm install -g yaml-language-server",
			},
		},
		{
			Name:        "json-language-server",
			Description: "JSON language server (npm: vscode-langservers-extracted)",
			Config: LSPConfig{
				Command:   "vscode-json-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".json", ".jsonc"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "vscode-langservers-extracted",
				Bin:      "vscode-json-language-server",
				Hint:     "npm install -g vscode-langservers-extracted",
			},
		},
		{
			Name:        "html-language-server",
			Description: "HTML language server (npm: vscode-langservers-extracted)",
			Config: LSPConfig{
				Command:   "vscode-html-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".html", ".htm"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "vscode-langservers-extracted",
				Bin:      "vscode-html-language-server",
				Hint:     "npm install -g vscode-langservers-extracted",
			},
		},
		{
			Name:        "css-language-server",
			Description: "CSS/SCSS/Less language server (npm: vscode-langservers-extracted)",
			Config: LSPConfig{
				Command:   "vscode-css-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".css", ".scss", ".sass", ".less"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "vscode-langservers-extracted",
				Bin:      "vscode-css-language-server",
				Hint:     "npm install -g vscode-langservers-extracted",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install marksman, or download a release",
				URL:      "https://github.com/artempyanykh/marksman/releases",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install jdtls, or download the Eclipse JDT LS release",
				URL:      "https://github.com/eclipse-jdtls/eclipse.jdt.ls",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "gem install solargraph",
				URL:      "https://solargraph.org",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "download a zls release matching your Zig version",
				URL:      "https://github.com/zigtools/zls#installation",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install kotlin-language-server, or download a release",
				URL:      "https://github.com/fwcd/kotlin-language-server",
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
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "intelephense",
				Bin:      "intelephense",
				Hint:     "npm install -g intelephense",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "download an OmniSharp release, or install the C# extension toolchain",
				URL:      "https://github.com/OmniSharp/omnisharp-roslyn/releases",
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
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "install the Dart or Flutter SDK (the server ships with it)",
				URL:      "https://dart.dev/get-dart",
			},
		},
		{
			// Mason/asdf installs expose an "elixir-ls" shim on PATH; a
			// manual upstream release ships the launcher as
			// "language_server.sh" instead, in which case users should
			// override Command in their [LSP.elixir-ls] config.
			Name:        "elixir-ls",
			Description: "Elixir language server (ElixirLS)",
			Config: LSPConfig{
				Command:   "elixir-ls",
				Args:      []string{},
				Languages: []string{".ex", ".exs"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "download an ElixirLS release and expose its launcher as elixir-ls",
				URL:      "https://github.com/elixir-lsp/elixir-ls/releases",
			},
		},
		{
			Name:        "vue-language-server",
			Description: "Vue language server (@vue/language-server)",
			Config: LSPConfig{
				Command:   "vue-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".vue"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "@vue/language-server",
				Bin:      "vue-language-server",
				// The server type-checks through the TypeScript API and
				// declares it as a peer dependency.
				ExtraPackages: []string{"typescript"},
				Hint:          "npm install -g @vue/language-server typescript",
			},
		},
		{
			Name:        "svelte-language-server",
			Description: "Svelte language server (svelte-language-server)",
			Config: LSPConfig{
				Command:   "svelteserver",
				Args:      []string{"--stdio"},
				Languages: []string{".svelte"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "svelte-language-server",
				// The package exposes its binary under a different name, so it
				// must be staged instead of run through bunx/npx by name.
				Bin:  "svelteserver",
				Hint: "npm install -g svelte-language-server",
			},
		},
		{
			Name:        "astro-ls",
			Description: "Astro language server (@astrojs/language-server)",
			Config: LSPConfig{
				Command:   "astro-ls",
				Args:      []string{"--stdio"},
				Languages: []string{".astro"},
			},
			Install: LSPInstall{
				Strategy:      LSPInstallNpm,
				Package:       "@astrojs/language-server",
				Bin:           "astro-ls",
				ExtraPackages: []string{"typescript"},
				Hint:          "npm install -g @astrojs/language-server typescript",
			},
		},
		{
			Name:        "dockerfile-language-server",
			Description: "Dockerfile language server (dockerfile-language-server-nodejs)",
			Config: LSPConfig{
				Command:   "docker-langserver",
				Args:      []string{"--stdio"},
				Languages: []string{".dockerfile"},
				// Dockerfiles are matched by name; the extension carries the
				// stage name, if anything ("Dockerfile.dev").
				Filenames: []string{"Dockerfile", "Containerfile"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "dockerfile-language-server-nodejs",
				Bin:      "docker-langserver",
				Hint:     "npm install -g dockerfile-language-server-nodejs",
			},
		},
		{
			Name:        "graphql-lsp",
			Description: "GraphQL language server (graphql-language-service-cli)",
			Config: LSPConfig{
				Command:   "graphql-lsp",
				Args:      []string{"server", "-m", "stream"},
				Languages: []string{".graphql", ".gql"},
			},
			Install: LSPInstall{
				Strategy:      LSPInstallNpm,
				Package:       "graphql-language-service-cli",
				Bin:           "graphql-lsp",
				ExtraPackages: []string{"graphql"},
				Hint:          "npm install -g graphql-language-service-cli graphql",
			},
		},
		{
			Name:        "prisma-language-server",
			Description: "Prisma schema language server (@prisma/language-server)",
			Config: LSPConfig{
				Command:   "prisma-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".prisma"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "@prisma/language-server",
				Bin:      "prisma-language-server",
				Hint:     "npm install -g @prisma/language-server",
			},
		},
		{
			Name:        "vim-language-server",
			Description: "Vimscript language server (vim-language-server)",
			Config: LSPConfig{
				Command:   "vim-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".vim"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "vim-language-server",
				Bin:      "vim-language-server",
				Hint:     "npm install -g vim-language-server",
			},
		},
		{
			// Opt-in: the ESLint server reports nothing without a project
			// configuration and would otherwise start next to
			// typescript-language-server on every JavaScript file.
			Name:        "eslint-language-server",
			Description: "ESLint language server (npm: vscode-langservers-extracted)",
			OptIn:       true,
			Config: LSPConfig{
				Command:   "vscode-eslint-language-server",
				Args:      []string{"--stdio"},
				Languages: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".vue", ".svelte"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "vscode-langservers-extracted",
				Bin:      "vscode-eslint-language-server",
				Hint:     "npm install -g vscode-langservers-extracted",
			},
		},
		{
			// Opt-in for the same reason as ESLint: Biome is a linter and
			// competes with the general-purpose servers for the same files.
			Name:        "biome",
			Description: "Biome linter/formatter language server (@biomejs/biome)",
			OptIn:       true,
			Config: LSPConfig{
				Command:   "biome",
				Args:      []string{"lsp-proxy"},
				Languages: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".json", ".jsonc", ".css"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "@biomejs/biome",
				Bin:      "biome",
				Hint:     "npm install -g @biomejs/biome",
			},
		},
		{
			// Opt-in: without a .sqllsrc.json describing the database the
			// server offers little beyond syntax checks.
			Name:        "sql-language-server",
			Description: "SQL language server (sql-language-server)",
			OptIn:       true,
			Config: LSPConfig{
				Command:   "sql-language-server",
				Args:      []string{"up", "--method", "stdio"},
				Languages: []string{".sql"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallNpm,
				Package:  "sql-language-server",
				Bin:      "sql-language-server",
				Hint:     "npm install -g sql-language-server",
			},
		},
		{
			// Opt-in: Deno claims the same files as
			// typescript-language-server and only one of them is right for a
			// given project.
			Name:        "deno",
			Description: "Deno language server (deno lsp)",
			OptIn:       true,
			Config: LSPConfig{
				Command:   "deno",
				Args:      []string{"lsp"},
				Languages: []string{".ts", ".tsx", ".js", ".jsx", ".mjs"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "install Deno (the language server ships with it)",
				URL:      "https://docs.deno.com/runtime/getting_started/installation/",
			},
		},
		{
			Name:        "terraform-ls",
			Description: "Terraform language server (terraform-ls)",
			Config: LSPConfig{
				Command:   "terraform-ls",
				Args:      []string{"serve"},
				Languages: []string{".tf", ".tfvars"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install hashicorp/tap/terraform-ls, or download a release",
				URL:      "https://github.com/hashicorp/terraform-ls",
			},
		},
		{
			Name:        "taplo",
			Description: "TOML language server (taplo)",
			Config: LSPConfig{
				Command:   "taplo",
				Args:      []string{"lsp", "stdio"},
				Languages: []string{".toml"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install taplo, or cargo install taplo-cli --features lsp",
				URL:      "https://taplo.tamasfe.dev/cli/introduction.html",
			},
		},
		{
			Name:        "texlab",
			Description: "LaTeX language server (texlab)",
			Config: LSPConfig{
				Command:   "texlab",
				Args:      []string{},
				Languages: []string{".tex", ".bib", ".sty", ".cls"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install texlab, or cargo install texlab",
				URL:      "https://github.com/latex-lsp/texlab",
			},
		},
		{
			Name:        "nixd",
			Description: "Nix language server (nixd)",
			Config: LSPConfig{
				Command:   "nixd",
				Args:      []string{},
				Languages: []string{".nix"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "nix profile install nixpkgs#nixd",
				URL:      "https://github.com/nix-community/nixd",
			},
		},
		{
			Name:        "clojure-lsp",
			Description: "Clojure language server (clojure-lsp)",
			Config: LSPConfig{
				Command:   "clojure-lsp",
				Args:      []string{},
				Languages: []string{".clj", ".cljs", ".cljc", ".edn"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "brew install clojure-lsp/brew/clojure-lsp-native, or download a release",
				URL:      "https://clojure-lsp.io/installation/",
			},
		},
		{
			Name:        "ocamllsp",
			Description: "OCaml language server (ocaml-lsp-server)",
			Config: LSPConfig{
				Command:   "ocamllsp",
				Args:      []string{},
				Languages: []string{".ml", ".mli"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "opam install ocaml-lsp-server",
				URL:      "https://github.com/ocaml/ocaml-lsp",
			},
		},
		{
			Name:        "haskell-language-server",
			Description: "Haskell language server (haskell-language-server)",
			Config: LSPConfig{
				Command:   "haskell-language-server-wrapper",
				Args:      []string{"--lsp"},
				Languages: []string{".hs", ".lhs"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "ghcup install hls",
				URL:      "https://haskell-language-server.readthedocs.io/en/latest/installation.html",
			},
		},
		{
			Name:        "sourcekit-lsp",
			Description: "Swift/Objective-C language server (sourcekit-lsp)",
			Config: LSPConfig{
				Command:   "sourcekit-lsp",
				Args:      []string{},
				Languages: []string{".swift"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "install Xcode or a Swift toolchain (the server ships with it)",
				URL:      "https://github.com/swiftlang/sourcekit-lsp",
			},
		},
		{
			Name:        "gleam",
			Description: "Gleam language server (gleam lsp)",
			Config: LSPConfig{
				Command:   "gleam",
				Args:      []string{"lsp"},
				Languages: []string{".gleam"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "install Gleam (the language server ships with the compiler)",
				URL:      "https://gleam.run/getting-started/installing/",
			},
		},
		{
			Name:        "metals",
			Description: "Scala language server (Metals)",
			Config: LSPConfig{
				Command:   "metals",
				Args:      []string{},
				Languages: []string{".scala", ".sc", ".sbt"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "coursier install metals",
				URL:      "https://scalameta.org/metals/docs/editors/user-configuration",
			},
		},
		{
			Name:        "ruby-lsp",
			Description: "Ruby language server (ruby-lsp)",
			Config: LSPConfig{
				Command:   "ruby-lsp",
				Args:      []string{},
				Languages: []string{".rb", ".rake", ".gemspec", ".ru"},
				Filenames: []string{"Gemfile", "Rakefile"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "gem install ruby-lsp",
				URL:      "https://shopify.github.io/ruby-lsp/",
			},
		},
		{
			Name:        "lemminx",
			Description: "XML language server (Eclipse LemMinX)",
			Config: LSPConfig{
				Command:   "lemminx",
				Args:      []string{},
				Languages: []string{".xml", ".xsd", ".xsl", ".xslt", ".dtd"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "download a LemMinX binary release",
				URL:      "https://github.com/eclipse-lemminx/lemminx",
			},
		},
		{
			Name:        "cmake-language-server",
			Description: "CMake language server (cmake-language-server)",
			Config: LSPConfig{
				Command:   "cmake-language-server",
				Args:      []string{},
				Languages: []string{".cmake"},
				Filenames: []string{"CMakeLists.txt"},
			},
			Install: LSPInstall{
				Strategy: LSPInstallManual,
				Hint:     "pipx install cmake-language-server",
				URL:      "https://github.com/regen100/cmake-language-server",
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
