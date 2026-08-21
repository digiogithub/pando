# Extension builds: `xpando` and `MODULE_TAGS`

Pando extensions are Go packages linked into the binary at build time, the way
Caddy modules are. There is no plugin file to drop into a directory and no
runtime loading: *installing* an extension means producing a binary that
contains it. Two tools cover that, and they answer different questions.

| | `make` + `MODULE_TAGS` | `xpando build` |
|---|---|---|
| Builds | this repository | a generated module that imports this repository |
| Links extensions from | nothing extra | the modules named by `--with` |
| Used for | core releases, both variants | composed (enterprise) binaries |

Configuration under `[Extensions]` only chooses which of the *compiled-in*
extensions load. A binary that was not built with an extension cannot enable it,
by design: that is what makes the boundary a licensing boundary and not a
runtime flag.

## `MODULE_TAGS` — the variant axis in the Makefile

`MODULE_TAGS` is the single knob. It feeds the build tags, the version stamp and
the artifact names together, so a variant can never be half-applied — a binary
built with the tags always reports the matching variant:

```bash
make build                             # ./pando,             variant ""
make build MODULE_TAGS=enterprise      # ./pando-enterprise,  variant "enterprise"
make build-enterprise                  # the same, spelled as a target
make release-enterprise                # dist/pando-enterprise-<platform>.zip
```

The variant is stamped into `internal/version.Variant` and is visible wherever
the build identifies itself:

```console
$ ./pando-enterprise --version
v0.9.1 (enterprise)
$ ./pando-enterprise extensions list
Pando v0.9.1 (enterprise build)
```

Standard builds print exactly what they always printed, so anything parsing
`pando --version` is unaffected.

Note what `MODULE_TAGS=enterprise` does *not* do: it links no private module.
The public repository contains no blank-import file for private packages —
adding one would put a `require` on a private module into the public `go.mod`
and break `go mod tidy` for everyone else. The tag only selects tag-guarded code
inside the core. Composition is `xpando`'s job.

## `xpando build` — composing a binary

`xpando` generates a throwaway main module whose `main.go` blank imports each
requested extension package and calls `cmd.Main()`, resolves it with the normal
Go toolchain, and builds it.

```bash
make xpando

./xpando build v0.9.1 \
    --with github.com/digiogithub/alchemai-agent/tools \
    --output ./pando-enterprise
```

| Flag | Meaning |
|---|---|
| `--with module[/pkg][@version][=/local/path]` | Package to blank import. Repeatable. A `=path` suffix builds against a local checkout; the replace targets the module root read from that checkout's `go.mod`, not the import path. |
| `--replace module[@version]=replacement` | A `go.mod` replace with no import. Repeatable. |
| `--tags tag1,tag2` | Build tags. |
| `--output path` | Output binary. Default `./pando`. |
| `--ldflags flags` | Appended after the flags xpando synthesises, so it can override them. |
| `--variant name` | Overrides the variant stamp. `--with` implies `enterprise`; `--variant ""` suppresses it. |
| `--keep` | Keep the generated module for inspection (also `XPANDO_SKIP_CLEANUP=1`). |

`GOOS`, `GOARCH`, `GOARM`, `CGO_ENABLED`, `CC` and `CXX` are passed through from
the environment, so cross-compiling works exactly as it does for `go build`.

The core is resolved first and explicitly. Resolving it after the extensions
would let an extension's own requirement drag the core to a version nobody
asked for, which is not a decision a build tool should make silently.

### The development loop

Point both the core and the extension at local checkouts:

```bash
./xpando build \
    --with github.com/digiogithub/alchemai-agent/tools=../alchemai-agent \
    --replace github.com/digiogithub/pando=. \
    --output ./pando-dev
```

A locally replaced core must not also be given a version — xpando rejects that
rather than pinning a version the replace then ignores.

### Private modules

Nothing xpando-specific: the Go toolchain fetches them, so it needs the usual
setup.

```bash
export GOPRIVATE=github.com/digiogithub/alchemai-agent
git config --global url."git@github.com-josedigio:digiogithub/".insteadOf "https://github.com/digiogithub/"
```

`xpando` is internal build tooling — customers receive a compiled binary and
never run it. That is why there is no module registry, lockfile, checksum
database or signature verification here: every module it links is one we
already control.

### Caveat: the frontend

The WebUI is embedded into the core module at *its* publish time
(`internal/api/webui/dist`), and `//go:embed` cannot cross module boundaries. A
composed binary therefore ships the stock frontend unless an extension module
supplies its own assets — which is what `FrontendProvider` and
`FrontendReplacer` are for. See [extension-frontend.md](extension-frontend.md).

## CI

`.github/workflows/build-matrix.yml` compiles and vets every tag set we ship and
asserts the variant stamp matches, so a change that only builds under the
default tags cannot reach `main`. It links no private module and needs no
credentials.
