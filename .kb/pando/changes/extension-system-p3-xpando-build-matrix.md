---
created_at: 2026-08-21T16:19:33.082931171Z
updated_at: 2026-08-21T16:19:33.082931171Z
tags:
    - change
    - extensions
    - build
    - xpando
    - enterprise
    - p3
---
# P3 — `cmd/xpando` builder + build matrix

Date: 2026-08-21
Status: IMPLEMENTED and verified end to end.
Roadmap: §9 of [[pando/analysis/extension_system_enterprise_analysis]], phase P3.
Builds on [[pando/changes/extension-system-p0-foundations]],
[[pando/changes/extension-system-p1-tools-commands]],
[[pando/changes/extension-system-p2-http-events]].

## What was changed

### 1. `cmd.Main()` — one entry point for every Pando binary

New `cmd/entrypoint.go` exports `Main()`, which installs the top-level
`logging.RecoverPanic` handler and calls `Execute()`. The repository's `main.go`
is now a single call to it.

Reason: the roadmap assumed the generated main module would call
`cmd.Execute()`, but `internal/logging` is unreachable from another module, so a
generated main could only call `Execute` and would **silently lose the panic
handler**. Exporting the whole entry point keeps a composed binary behaviourally
identical to the repository build, and gives one place for anything that must
happen in every Pando binary.

### 2. `cmd/xpando` — the builder (xcaddy equivalent)

New `package main` at `cmd/xpando/main.go` (~430 LOC with docs) plus
`main_test.go` (14 tests).

```
xpando build [core-version]
    --with     module[/pkg][@version][=/local/path]   (repeatable)
    --replace  module[@version]=replacement           (repeatable)
    --tags     enterprise,cuda
    --output   ./pando-enterprise
    --ldflags  "-X ...=..."
    --variant  enterprise
    --keep                                            (or XPANDO_SKIP_CLEANUP=1)
```

Pipeline: temp dir → generate `main.go` (blank imports + `pandocmd.Main()`) →
`go mod init xpando.build/composed` → `go mod edit -replace` for every local
checkout and every `--replace` → `go get` core (first, explicitly) → `go get`
each remote `--with` → `go mod tidy` → `go build -tags … -ldflags …` → cleanup.
`GOOS/GOARCH/GOARM/CGO_ENABLED/CC/CXX` pass through via `os.Environ()`.

Design decisions worth keeping:

- **Core resolved first and explicitly.** Resolving it after the extensions lets
  an extension's own requirement drag the core to a version nobody asked for.
  A build tool must not make that choice silently.
- **A local `--with` replaces the module root, not the import path.**
  `--with example.com/mod/tools=/path` reads `module` from `/path/go.mod` and
  replaces `example.com/mod`; replacing the import path would resolve nothing.
  The import path is verified to be inside that module (`withinModule`), so a
  mismatched pair errors instead of producing a confusing tidy failure.
- **`replaced()` short-circuits `go get` for a locally replaced core**, and
  rejects `coreVersion` + local core together rather than pinning a version the
  replace then ignores.
- **`--with` implies `--variant enterprise`.** A binary composed with extra
  modules is not a stock build and must not report itself as one. An explicit
  `--variant` always wins, including an explicit empty one.
- **A named core version also stamps `version.Version`**, unless the caller's
  `--ldflags` already sets it — otherwise a composed release build reports
  `unknown`.
- **Hand-rolled flag parsing**, not `flag`: `--with` must stay ordered and
  repeatable and its value legitimately contains `=` and `@`.
- No registry, lockfile, checksum or signature machinery: xpando is *internal*
  build tooling (§8.6-Q3), every module it links is one we control.

### 3. Variant stamping reaches `--version`

`internal/version.Display()` returns `Normalize()` for a standard build and
`"<version> (<variant>)"` otherwise; `cmd/root.go` uses it for `--version`.
Standard output is byte-identical to before, so anything parsing
`pando --version` is unaffected — only a non-standard binary looks different,
which is the point.

### 4. Makefile — `MODULE_TAGS` as the single variant knob

```make
MODULE_TAGS   ?=
GO_BUILD_TAGS  = $(strip $(MODULE_TAGS))
GO_TAGS_FLAG   = $(if $(GO_BUILD_TAGS),-tags "$(GO_BUILD_TAGS)",)
VARIANT       := $(if $(findstring enterprise,$(MODULE_TAGS)),enterprise,)
DIST_SUFFIX   := $(if $(VARIANT),-$(VARIANT),)
VARIANT_LDFLAGS := $(if $(VARIANT),-X …/internal/version.Variant=$(VARIANT),)
LDFLAGS := -s -w -X …/internal/version.Version=$(VERSION) $(VARIANT_LDFLAGS)
```

Threaded through `build`, `build-fast`, `test-integration` and the
`build_release` macro (all five `release-*` rules inherit it). Artifacts are
`pando[-variant]-<platform>` so both variants can share `dist/`. New targets:
`build-enterprise`, `release-enterprise`, `xpando`.

`VARIANT_LDFLAGS` is kept separate from `LDFLAGS` so even the unstamped
`build-fast` carries the variant: a binary with the tags that calls itself
standard is worse than no stamp at all.

Documented in the Makefile: `MODULE_TAGS=enterprise` in this repository links
**no** private module. There is deliberately no blank-import file in the public
repo (it would put a `require` on a private module into the public `go.mod` and
break `go mod tidy` for OSS users) — composition is xpando's generated module.

### 5. CI

New `.github/workflows/build-matrix.yml`: matrix over `MODULE_TAGS` `''` and
`enterprise`, running `go vet ./...` with the tags, `make build-fast`, an
assertion that `--version` reports the matching variant (and that a standard
build reports none), `make xpando`, and the extension test packages. Links no
private module, so it needs no credentials.

### 6. Docs

New `docs/extension-builds.md` (when to use `make MODULE_TAGS=` vs
`xpando build`, full flag table, dev loop, `GOPRIVATE`/`insteadOf` setup, the
`go:embed` frontend caveat, CI). README's "Building from Source" links to it.
`.gitignore` covers `/pando-enterprise` and `/xpando`.

## Files touched

- new: `cmd/entrypoint.go`, `cmd/xpando/main.go`, `cmd/xpando/main_test.go`,
  `.github/workflows/build-matrix.yml`, `docs/extension-builds.md`
- modified: `main.go`, `cmd/root.go` (`--version`), `internal/version/version.go`
  (+`Display`), `internal/version/version_test.go`, `Makefile`, `README.md`,
  `.gitignore`

## Verification

- `go build ./...`, `gofmt`, `go vet` clean on every touched package.
  (`cmd/test_ollama_main/main.go` is unformatted, pre-existing, untouched.)
- `go test ./cmd/... ./internal/version ./internal/extensions ./pkg/extension
  ./internal/commands ./internal/api ./internal/app ./internal/config` all pass;
  15 new tests (14 xpando + `TestDisplay`).
- Variant plumbing:
  `make build-fast MODULE_TAGS=enterprise` → `./pando-enterprise --version` →
  `v0.647.7-… (enterprise)`; `make build-fast` → `./pando --version` → no
  variant.
- **End-to-end composition** with a throwaway module `example.com/demoext`
  (scratchpad) registering an Enterprise `tools.demo.xp3` extension with a
  `Commands()` capability:
  ```
  xpando build --with example.com/demoext/tools=<path> \
               --replace github.com/digiogithub/pando=/www/MCP/Pando/pando \
               --output ./pando-xp3
  ```
  Result: `pando-xp3 --version` → `unknown (enterprise)` (variant auto-stamped);
  `extensions list` → `tools.demo.xp3 1.0.0 Enterprise disabled` (non-MIT
  defaults off); after `[Extensions.Entries."tools.demo.xp3"] Enabled = true` in
  `.pando.toml` → state `loaded` and `pando-xp3 ext ping` → `pong from xp3`.

Not exercised: an actual cross-compile (`GOOS`/`GOARCH` are plain
`os.Environ()` passthrough) and a real private-module fetch over `GOPRIVATE`.

## Next

P4 — frontend capability (`FrontendProvider`, `/api/v1/extensions/ui`, dynamic
ESM panel loading). Note the `go:embed` module-boundary constraint recorded
above and in §7.3.
