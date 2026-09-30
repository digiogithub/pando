# Installation

## Install from binaries

Installer script for windows (copy into powershell)

```
iex (irm https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-windows.ps1)
```

Installer in linux

```
curl -fsSL https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-linux.sh | bash
```

In OSX download the [release](https://github.com/digiogithub/pando/releases) `.pkg` for your architecture — `pando-<version>-darwin-arm64.pkg` (Apple Silicon) or `pando-<version>-darwin-x64.pkg` (Intel). The installer places `Pando.app` (with the embedded desktop wrapper, icons and the `/usr/local/bin/pando` launcher) under `/Applications`.

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
