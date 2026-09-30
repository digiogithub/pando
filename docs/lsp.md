# Language Servers (LSP)

Pando activates language servers **on demand**: nothing starts at boot. When it
edits a file, it looks at the file's extension (or its name, for files like
`Dockerfile`), picks the matching server from a built-in catalogue of 45 presets
and starts it. A non-Go project never spins up `gopls`, and a session that edits
nothing starts no language server at all.

When the server's binary is missing, Pando can **install it itself**: servers
distributed as npm packages (pyright, typescript-language-server, vue, svelte,
astro, dockerfile, yaml/json/html/css, bash, intelephense, …) are installed with
`bun` or `npm` — in that order of preference — into `~/.config/pando/lsp`, never
into your project. Servers that ship with a language toolchain (gopls,
rust-analyzer, clangd, jdtls, …) are never installed automatically: Pando
reports the exact command you should run instead.

The `diagnostics` tool waits for the server to be **ready** rather than merely
spawned, extending its wait while an install is in flight, and tells you what to
do when no server can be obtained:

```
no language server available for .py files: pyright-langserver is not installed
and neither bun nor node is available to install it (run: npm install -g pyright)
```

## What triggers an activation

`LSPActivateOn` decides which events may start a server:

| Value         | Starts a server for…                                                         |
| ------------- | ---------------------------------------------------------------------------- |
| `"edits"`     | files Pando edits, plus an explicit `diagnostics` call *(default)*            |
| `"reads"`     | also files it views, opens in the editor, or browses in the file tree        |
| `"workspace"` | also files changed outside Pando (enables a workspace-wide watcher)          |
| `"off"`       | nothing starts on demand                                                     |

## Configuration

You only need `[LSP]` entries to **override, extend, or disable** a catalogue
server, to enable an opt-in one, or to add a server Pando doesn't know about:

```toml
LSPAutoActivate   = true      # false disables on-demand activation entirely
LSPActivateOn     = "edits"   # edits | reads | workspace | off
LSPAutoInstall    = true      # install npm-distributed servers with bun/npm
LSPRunner         = "auto"    # auto | bun | npm | off
LSPStartupTimeout = "20s"     # how long a tool waits for a server to be ready
LSPInstallTimeout = "120s"    # extended wait while a server is being installed

# Override a preset's command / args, or add your own server.
[LSP.gopls]
Command   = "gopls"
Args      = []
Languages = ["go"]
Filenames = []       # base names handled regardless of extension
Disabled  = false    # true keeps this server from ever starting
Autostart = false    # true starts it eagerly at boot instead of on demand
```

| Setting             | Scope      | Meaning                                                                    |
| ------------------- | ---------- | -------------------------------------------------------------------------- |
| `LSPAutoActivate`   | global     | When `true` (default), start servers on demand.                            |
| `LSPActivateOn`     | global     | Which events may start a server (see the table above).                     |
| `LSPAutoInstall`    | global     | Allow Pando to install npm-distributed servers with bun/npm.               |
| `LSPRunner`         | global     | Which package manager to use (see the table below).                        |
| `LSPStartupTimeout` | global     | How long a tool waits for a lazily started server to become ready.         |
| `LSPInstallTimeout` | global     | Extended wait while a server is being installed (never below the startup wait). |
| `Command` / `Args`  | per-server | Override the preset's executable and arguments.                            |
| `Languages`         | per-server | File extensions the server handles.                                        |
| `Filenames`         | per-server | Base names it handles regardless of extension (`Dockerfile`, `CMakeLists.txt`). |
| `Disabled`          | per-server | Never start this server, even on demand.                                   |
| `Autostart`         | per-server | Eagerly start this server at boot.                                         |

Overriding `Command` with your own binary also drops the preset's install
recipe: Pando never installs a package for an executable you chose yourself.

## Choosing the package manager

`LSPRunner` selects the JavaScript toolchain used to install and run
npm-distributed servers:

| Value    | Behaviour                                                                        |
| -------- | -------------------------------------------------------------------------------- |
| `"auto"` | bun when it is installed, npm/npx otherwise *(default)*                          |
| `"bun"`  | only bun — npm is never used, even when it is the only toolchain present         |
| `"npm"`  | only npm/npx, even when bun is installed                                          |
| `"off"`  | neither: npm-distributed servers are only used when their binary is already on PATH (or was staged by an earlier run) |

`"off"` also disables automatic installation, whatever `LSPAutoInstall` says:
without bun or npm there is nothing to install with. Servers that ship with a
language toolchain (gopls, rust-analyzer, clangd, …) are unaffected by this
setting.

## Seeing what is available

The **Settings → LSP** page (TUI and web UI) lists the activation settings and,
for every server, whether it is `installed`, `installable (bun/npm)` or needs a
manual install — with the command to run in that last case. The agent can read
the same report itself:

```
pando_setup lsp             # configured, running and installed servers
pando_setup lsp --all       # the whole catalogue, opt-in servers included
pando_setup lsp --missing   # only what needs a manual install
pando_setup lsp pyright     # one server in detail
```

## Built-in preset catalogue

Servers marked **npm** are installed by Pando on first use; the others print
their install command when they are missing.

| Preset name                   | Command                        | Files                                                        | Install |
| ----------------------------- | ------------------------------ | ------------------------------------------------------------ | ------- |
| `gopls`                       | `gopls`                        | `.go`                                                        | manual  |
| `rust-analyzer`               | `rust-analyzer`                | `.rs`                                                        | manual  |
| `typescript-language-server`  | `typescript-language-server`   | `.ts` `.tsx` `.js` `.jsx` `.mjs` `.cjs`                       | npm     |
| `pyright`                     | `pyright-langserver`           | `.py` `.pyi`                                                 | npm     |
| `pylsp`                       | `pylsp`                        | `.py` `.pyi`                                                 | manual  |
| `clangd`                      | `clangd`                       | `.c` `.cc` `.cpp` `.cxx` `.c++` `.h` `.hh` `.hpp` `.m` `.mm`  | manual  |
| `lua-language-server`         | `lua-language-server`          | `.lua`                                                       | manual  |
| `bash-language-server`        | `bash-language-server`         | `.sh` `.bash` `.zsh` `.ksh`                                   | npm     |
| `yaml-language-server`        | `yaml-language-server`         | `.yaml` `.yml`                                               | npm     |
| `json-language-server`        | `vscode-json-language-server`  | `.json` `.jsonc`                                             | npm     |
| `html-language-server`        | `vscode-html-language-server`  | `.html` `.htm`                                               | npm     |
| `css-language-server`         | `vscode-css-language-server`   | `.css` `.scss` `.sass` `.less`                                | npm     |
| `marksman`                    | `marksman`                     | `.md` `.markdown`                                            | manual  |
| `jdtls`                       | `jdtls`                        | `.java`                                                      | manual  |
| `solargraph`                  | `solargraph`                   | `.rb` `.rake`                                                | manual  |
| `ruby-lsp`                    | `ruby-lsp`                     | `.rb` `.rake` `.gemspec` `.ru`, `Gemfile`, `Rakefile`         | manual  |
| `zls`                         | `zls`                          | `.zig`                                                       | manual  |
| `kotlin-language-server`      | `kotlin-language-server`       | `.kt` `.kts`                                                 | manual  |
| `intelephense`                | `intelephense`                 | `.php`                                                       | npm     |
| `omnisharp`                   | `omnisharp`                    | `.cs`                                                        | manual  |
| `dartls`                      | `dart`                         | `.dart`                                                      | manual  |
| `elixir-ls`                   | `elixir-ls`                    | `.ex` `.exs`                                                 | manual  |
| `vue-language-server`         | `vue-language-server`          | `.vue`                                                       | npm     |
| `svelte-language-server`      | `svelteserver`                 | `.svelte`                                                    | npm     |
| `astro-ls`                    | `astro-ls`                     | `.astro`                                                     | npm     |
| `dockerfile-language-server`  | `docker-langserver`            | `.dockerfile`, `Dockerfile`, `Containerfile`                  | npm     |
| `graphql-lsp`                 | `graphql-lsp`                  | `.graphql` `.gql`                                            | npm     |
| `prisma-language-server`      | `prisma-language-server`       | `.prisma`                                                    | npm     |
| `vim-language-server`         | `vim-language-server`          | `.vim`                                                       | npm     |
| `terraform-ls`                | `terraform-ls`                 | `.tf` `.tfvars`                                              | manual  |
| `taplo`                       | `taplo`                        | `.toml`                                                      | manual  |
| `texlab`                      | `texlab`                       | `.tex` `.bib` `.sty` `.cls`                                   | manual  |
| `nixd`                        | `nixd`                         | `.nix`                                                       | manual  |
| `clojure-lsp`                 | `clojure-lsp`                  | `.clj` `.cljs` `.cljc` `.edn`                                 | manual  |
| `ocamllsp`                    | `ocamllsp`                     | `.ml` `.mli`                                                 | manual  |
| `haskell-language-server`     | `haskell-language-server-wrapper` | `.hs` `.lhs`                                              | manual  |
| `sourcekit-lsp`               | `sourcekit-lsp`                | `.swift`                                                     | manual  |
| `gleam`                       | `gleam`                        | `.gleam`                                                     | manual  |
| `metals`                      | `metals`                       | `.scala` `.sc` `.sbt`                                         | manual  |
| `lemminx`                     | `lemminx`                      | `.xml` `.xsd` `.xsl` `.xslt` `.dtd`                           | manual  |
| `cmake-language-server`       | `cmake-language-server`        | `.cmake`, `CMakeLists.txt`                                   | manual  |

Four presets are **opt-in**: they stay off until you declare them, because they
need project configuration or would compete with a general-purpose server.
Adding the section — even an empty one — is enough to enable them:

| Opt-in preset            | Command                          | Files                                            | Why opt-in                              |
| ------------------------ | -------------------------------- | ------------------------------------------------ | --------------------------------------- |
| `eslint-language-server` | `vscode-eslint-language-server`  | `.js` `.jsx` `.ts` `.tsx` `.vue` `.svelte` …      | reports nothing without an ESLint config |
| `biome`                  | `biome`                          | `.js` `.ts` `.json` `.css` …                      | linter; duplicates the TS server         |
| `sql-language-server`    | `sql-language-server`            | `.sql`                                           | needs a database connection config       |
| `deno`                   | `deno`                           | `.ts` `.tsx` `.js` `.jsx` `.mjs`                  | claims the same files as the TS server   |

```toml
[LSP.biome]   # an empty section is enough to enable an opt-in preset
```

`json-language-server`, `html-language-server`, `css-language-server` and
`eslint-language-server` all come from the npm package
`vscode-langservers-extracted`. `elixir-ls` matches the shim name installed by
Mason/asdf; a manual upstream ElixirLS release instead ships
`language_server.sh`, so override `Command` under `[LSP.elixir-ls]` if you
installed it that way.
