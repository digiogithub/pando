# Installation

## Install from binaries

Every release publishes signed binaries on the
[releases page](https://github.com/digiogithub/pando/releases/latest):

| Platform | File |
|---|---|
| macOS (Apple Silicon / Intel) | `pando-<version>-darwin-arm64.pkg` / `pando-<version>-darwin-x64.pkg` (signed and notarized) |
| Linux (x86-64 / ARM64) | `pando-linux-x64.zip` / `pando-linux-arm64.zip` |
| Windows (x86-64) | `pando-windows-x64.zip` (Authenticode-signed) |

`SHA256SUMS` lists the SHA-256 of every file.

### Linux and macOS: install script

```
curl -fsSL https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install.sh | bash
```

- **Linux**: installs `~/.local/bin/pando`, a menu entry, and the GTK/WebKitGTK
  libraries the desktop window needs (asks for `sudo` only if some are missing;
  a failure there does not stop the install).
- **macOS**: downloads the `.pkg` for your architecture and runs the system
  installer, which places `Pando.app` (with the embedded desktop wrapper, icons
  and the `/usr/local/bin/pando` launcher) under `/Applications`.

Options go after `bash -s --`, or as environment variables:

| Option | Variable | Effect |
|---|---|---|
| `--version v1.2.7` | `PANDO_VERSION` | Install that release instead of the latest |
| `--dir <path>` | `PANDO_INSTALL_DIR` | Where the binary goes (default `~/.local/bin`) |
| `--no-desktop` | `PANDO_NO_DESKTOP=1` | Linux: no system packages, icon or menu entry (servers, CI, containers) |
| `--cli-only` | `PANDO_CLI_ONLY=1` | macOS: only the CLI binary, no `.pkg` |
| `--force` | `PANDO_FORCE=1` | Reinstall the same version |

```
curl -fsSL https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install.sh | bash -s -- --no-desktop
```

The download is checked against the release's `SHA256SUMS`. `scripts/install-linux.sh`
still works and forwards to `install.sh`.

### Windows: install script (PowerShell)

```
iex (irm https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-windows.ps1)
```

Installs to `%LOCALAPPDATA%\Programs\pando`, adds it to the user `PATH`, and
checks the SHA-256 and the Authenticode signature. Windows on ARM gets the x64
build.

## Using Go

```bash
go install github.com/digiogithub/pando@latest
```

## Building from Source

```bash
git clone https://github.com/digiogithub/pando.git
cd pando
cd web-ui && bun install && bun run build:embedded && cd ..
go build ./...
go build -o pando .
./pando app
```

`go build ./...` also works immediately after cloning; it embeds a tracked WebUI
placeholder until the real assets are generated. Generated desktop binaries
are embedded when present, without overwriting any existing build assets.

Extensions are linked in at build time. To write one, see
[docs/extension-authoring.md](extension-authoring.md); to decide whether
you want an extension at all rather than an MCP server, a Lua hook or a skill,
see [docs/extension-mechanisms.md](extension-mechanisms.md). To build a
variant, or to compose a binary from the core plus private extension modules
with `xpando`, see [docs/extension-builds.md](extension-builds.md).
Extensions can also contribute UI
([docs/extension-frontend.md](extension-frontend.md)) and observe or
augment the remembrance layer
([docs/extension-memory.md](extension-memory.md)).
