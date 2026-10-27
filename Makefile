# ============================================================
# Pando — top-level Makefile
# ============================================================

GOPATH ?= $(shell go env GOPATH)
VERSION ?= $(shell git describe --tags 2>/dev/null || echo dev)

# ------------------------------------------------------------
# Build variant
# ------------------------------------------------------------
# MODULE_TAGS is the single knob for the whole variant axis: it feeds the build
# tags, the version stamp and the artifact names, so a variant can never be
# half-applied (tagged sources but a binary that reports itself as standard).
#
# Building this repository with MODULE_TAGS=enterprise links NO private module:
# composition happens in a generated main module produced by `xpando build`
# (see cmd/xpando). The tag only selects tag-guarded code inside the core, and
# the stamp records which kind of binary this is.
MODULE_TAGS   ?=
GO_BUILD_TAGS  = $(strip $(MODULE_TAGS))
GO_TAGS_FLAG   = $(if $(GO_BUILD_TAGS),-tags "$(GO_BUILD_TAGS)",)
VARIANT       := $(if $(findstring enterprise,$(MODULE_TAGS)),enterprise,)
DIST_SUFFIX   := $(if $(VARIANT),-$(VARIANT),)

# The variant stamp is separate from the release stamp so that even the
# unstamped fast build reports which variant it is. A binary that carries the
# tags but calls itself standard is worse than no stamp at all.
VARIANT_LDFLAGS := $(if $(VARIANT),-X github.com/digiogithub/pando/internal/version.Variant=$(VARIANT),)
# TELEMETRY_LDFLAGS embeds the Better Stack ingest source token into the
# binary at link time, so it is never committed and never stored in user
# config (see internal/telemetry/build.go). Only set when the caller supplies
# PANDO_BETTERSTACK_TOKEN, so a token-less build (CI PR builds, `go install`,
# forks) carries no empty -X flag and simply reports telemetry as unavailable.
#   Local dev: PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build
# NOTE: PANDO_BETTERSTACK_TOKEN is interpolated unquoted-inside-single-quotes
# into the -ldflags string below (LDFLAGS := ... -ldflags '$(LDFLAGS)' ...).
# It must never contain a single quote ('), which would break out of that
# shell-quoted argument — Better Stack source tokens are alphanumeric, so
# this is not expected to matter in practice, but it is exactly the kind of
# thing worth flagging for anyone tempted to source this value from
# somewhere less trusted than `kvage`/a CI secret.
TELEMETRY_LDFLAGS :=
ifneq ($(PANDO_BETTERSTACK_TOKEN),)
TELEMETRY_LDFLAGS := -X github.com/digiogithub/pando/internal/telemetry.sourceToken=$(PANDO_BETTERSTACK_TOKEN)
endif
LDFLAGS := -s -w -X github.com/digiogithub/pando/internal/version.Version=$(VERSION) $(VARIANT_LDFLAGS) $(TELEMETRY_LDFLAGS)
DIST_DIR := dist
WEB_UI_DIR := web-ui
WEB_UI_INSTALL_CMD ?= bun install
WEB_UI_EMBEDDED_BUILD_CMD ?= bun run build:embedded
CGO_ENABLED ?= 1
ZIG ?= zig
UPX ?= upx
CC_LINUX_ARM64 ?= $(ZIG) cc -target aarch64-linux-gnu
CXX_LINUX_ARM64 ?= $(ZIG) c++ -target aarch64-linux-gnu
CC_WINDOWS_AMD64 ?= $(ZIG) cc -target x86_64-windows-gnu
CXX_WINDOWS_AMD64 ?= $(ZIG) c++ -target x86_64-windows-gnu
MACOS_MIN_VERSION ?= 11.0
# On Darwin, use native Apple clang for cross-arch builds (zig cannot resolve macOS system libs).
# On Linux/CI, fall back to zig for darwin targets.
ifeq ($(shell uname),Darwin)
CC_DARWIN_AMD64 ?= clang -arch x86_64
CXX_DARWIN_AMD64 ?= clang++ -arch x86_64
CC_DARWIN_ARM64 ?= clang -arch arm64
CXX_DARWIN_ARM64 ?= clang++ -arch arm64
else
CC_DARWIN_AMD64 ?= $(ZIG) cc -target x86_64-macos.$(MACOS_MIN_VERSION)
CXX_DARWIN_AMD64 ?= $(ZIG) c++ -target x86_64-macos.$(MACOS_MIN_VERSION)
CC_DARWIN_ARM64 ?= $(ZIG) cc -target aarch64-macos.$(MACOS_MIN_VERSION)
CXX_DARWIN_ARM64 ?= $(ZIG) c++ -target aarch64-macos.$(MACOS_MIN_VERSION)
endif
MACOS_SYSROOT_FLAGS :=
# Hardened-runtime signing wrapper applied to the darwin release binaries
# (reads SIGNING_IDENTITY or MACOS_SIGN_IDENTITY, MACOS_SIGN_KEYCHAIN_PATH and
# optionally MACOS_ENTITLEMENTS from the environment; see scripts/codesign-macos).
MACOS_CODESIGN_WRAPPER ?= $(CURDIR)/scripts/codesign-macos

# ------------------------------------------------------------
# Native libraries embedded into the binary (internal/nativeui/natives)
# ------------------------------------------------------------
# libghostty-vt (the desktop terminal) is fetched per target into
# internal/nativeui/natives/<goos>-<goarch>/ and embedded with go:embed, so
# every distribution is one self-contained binary. On darwin the dylib is
# signed there with the same Developer ID as pando (MACOS_CODESIGN_WRAPPER,
# hardened runtime, timestamp) BEFORE go build: hardened-runtime library
# validation then accepts it without disable-library-validation.
# NATIVES_SIGN: auto (sign when SIGNING_IDENTITY/MACOS_SIGN_IDENTITY is set),
# required (fail without it; used by release-darwin-*) or off.
NATIVES_DIR     := internal/nativeui/natives
NATIVES_TARGETS := linux-amd64 linux-arm64 windows-amd64 windows-arm64 darwin-amd64 darwin-arm64
NATIVES_SIGN    ?= auto
HOST_NATIVES    := $(shell go env GOOS 2>/dev/null)-$(shell go env GOARCH 2>/dev/null)

.PHONY: build-enterprise release-enterprise xpando embed-stubs build build-fast web-ui-embedded dist-clean natives natives-host natives-clean $(addprefix natives-,$(NATIVES_TARGETS)) release release-linux-amd64 release-linux-arm64 release-windows-amd64 release-darwin-amd64 release-darwin-arm64 test-integration help

## Create the generated go:embed placeholder for the web UI (fresh checkout / CI).
## internal/api/webui/dist is a gitignored build artifact, but its embed
## pattern must still resolve for `go vet`/`go build` before the real assets
## are generated. Existing files are never overwritten.
embed-stubs:
	@mkdir -p internal/api/webui/dist
	@[ -f internal/api/webui/dist/index.html ] || printf '<!doctype html><title>pando</title>\n' > internal/api/webui/dist/index.html

## Build embedded web-ui assets used by the API binary
web-ui-embedded:
	cd $(WEB_UI_DIR) && $(WEB_UI_INSTALL_CMD) && $(WEB_UI_EMBEDDED_BUILD_CMD)

## Fetch libghostty-vt for natives-<goos>-<goarch> into the go:embed dir (darwin: signed, see NATIVES_SIGN)
$(addprefix natives-,$(NATIVES_TARGETS)): natives-%:
	@set -e; out="$$(scripts/fetch-ghostty-vt $* $(NATIVES_DIR)/$*)"; echo "Embedding $$out"; \
	case "$*" in darwin-*) \
		if [ "$(NATIVES_SIGN)" = off ]; then echo "NATIVES_SIGN=off: $$out left with its upstream signature"; \
		elif [ -n "$${SIGNING_IDENTITY:-$${MACOS_SIGN_IDENTITY:-}}" ] && command -v codesign >/dev/null 2>&1; then \
			echo "Signing $$out with $(MACOS_CODESIGN_WRAPPER)..."; "$(MACOS_CODESIGN_WRAPPER)" "$$out"; \
			codesign --verify --strict --verbose=2 "$$out"; \
		elif [ "$(NATIVES_SIGN)" = required ]; then \
			rm -f "$$out"; echo "Error: $$out must be signed (set SIGNING_IDENTITY and MACOS_SIGN_KEYCHAIN_PATH, on macOS)." >&2; exit 1; \
		else echo "Warning: $$out not signed with a Developer ID (no SIGNING_IDENTITY or codesign); a hardened-runtime pando will refuse to load it."; fi ;; \
	esac

## Fetch the native libraries for the host platform; offline it warns and builds with what is there
natives-host:
	@$(MAKE) --no-print-directory natives-$(HOST_NATIVES) || echo "Warning: could not fetch libghostty-vt for $(HOST_NATIVES); building without an embedded terminal library (MyGo downloads it at run time)."

## Fetch the native libraries for every release target
natives: $(addprefix natives-,$(NATIVES_TARGETS))

## Remove fetched native libraries (keeps the .keep placeholders)
natives-clean:
	find $(NATIVES_DIR) -mindepth 2 -type f ! -name .keep -delete

## Build local CLI binary with embedded web-ui, terminal library and release version from git tag
build: web-ui-embedded natives-host
	go build $(GO_TAGS_FLAG) -ldflags '$(LDFLAGS)' -o pando$(DIST_SUFFIX) .

## Build the enterprise-variant CLI binary (tags + version stamp + -enterprise suffix)
build-enterprise: MODULE_TAGS=enterprise
build-enterprise: build

## Build local CLI binary without rebuilding web-ui (fast iteration)
build-fast:
	go build $(GO_TAGS_FLAG) $(if $(strip $(VARIANT_LDFLAGS) $(TELEMETRY_LDFLAGS)),-ldflags '$(VARIANT_LDFLAGS) $(TELEMETRY_LDFLAGS)',) -o pando$(DIST_SUFFIX) .

## Build the xpando builder, which composes a binary from core + extension modules
xpando:
	go build -ldflags '$(LDFLAGS)' -o xpando ./cmd/xpando

## Run multi-process single-writer integration tests (requires compiled binary)
test-integration: build-fast
	PANDO_BINARY=$(CURDIR)/pando$(DIST_SUFFIX) go test -tags=integration ./test/integration/single_writer/... -v -timeout 180s

## Remove distribution artifacts
dist-clean:
	rm -rf $(DIST_DIR)

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

# $(1) GOOS  $(2) GOARCH  $(3) artifact platform name  $(4) binary extension  $(5) extra env
# The artifact is named pando[-variant]-<platform>, so a standard and an
# enterprise release can sit in the same dist/ without overwriting each other.
define build_release
	@echo "Building $(1)/$(2)$(if $(VARIANT), [$(VARIANT)],)..."
	GOOS=$(1) GOARCH=$(2) CGO_ENABLED=$(CGO_ENABLED) $(5) go build $(GO_TAGS_FLAG) -ldflags '$(LDFLAGS)' -o $(DIST_DIR)/pando$(DIST_SUFFIX)-$(3)$(4) .
	if [ "$(1)" != "darwin" ] && command -v $(UPX) >/dev/null 2>&1; then \
		$(UPX) --best --lzma $(DIST_DIR)/pando$(DIST_SUFFIX)-$(3)$(4); \
	else echo "Skipping UPX for $(3)"; fi
	if [ "$(1)" = "darwin" ]; then \
		_cs="$(MACOS_CODESIGN_WRAPPER)"; \
		if [ -x "$$_cs" ]; then \
			echo "Signing $(DIST_DIR)/pando$(DIST_SUFFIX)-$(3)$(4) with $$_cs..."; \
			"$$_cs" "$(DIST_DIR)/pando$(DIST_SUFFIX)-$(3)$(4)"; \
		fi; \
	fi
	cd $(DIST_DIR) && zip -qm pando$(DIST_SUFFIX)-$(3).zip pando$(DIST_SUFFIX)-$(3)$(4)
endef

define require_cmd
	@sh -c 'command -v "$$1" >/dev/null 2>&1 || { echo "Missing required tool: $$1"; exit 1; }' -- "$(1)"
endef

## Build all release archives in dist/ for Linux, Windows and macOS
release: web-ui-embedded $(DIST_DIR) release-linux-amd64 release-linux-arm64 release-windows-amd64 $(if $(filter Darwin,$(shell uname)),release-darwin-amd64 release-darwin-arm64)

## Build all release archives for the enterprise variant
release-enterprise: MODULE_TAGS=enterprise
release-enterprise: release

## Build Linux x64 release archive in dist/
release-linux-amd64: natives-linux-amd64 | $(DIST_DIR)
	$(call build_release,linux,amd64,linux-x64,)

## Build Linux arm64 release archive in dist/
release-linux-arm64: natives-linux-arm64 | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,linux,arm64,linux-arm64,,CC='$(CC_LINUX_ARM64)' CXX='$(CXX_LINUX_ARM64)')

## Build Windows x64 release archive in dist/
release-windows-amd64: natives-windows-amd64 | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,windows,amd64,windows-x64,.exe,CC='$(CC_WINDOWS_AMD64)' CXX='$(CXX_WINDOWS_AMD64)')

ifeq ($(shell uname),Darwin)
NATIVE_ARCH := $(shell uname -m)

## Build macOS x64 release archive in dist/ (signed; embeds the signed dylib)
release-darwin-amd64: NATIVES_SIGN=required
release-darwin-amd64: natives-darwin-amd64 | $(DIST_DIR)
ifeq ($(NATIVE_ARCH),x86_64)
	$(call build_release,darwin,amd64,darwin-x64,,)
else
	$(call require_cmd,$(ZIG))
	$(call build_release,darwin,amd64,darwin-x64,,CC='$(CC_DARWIN_AMD64)' CXX='$(CXX_DARWIN_AMD64)' $(MACOS_SYSROOT_FLAGS))
endif

## Build macOS arm64 release archive in dist/ (signed; embeds the signed dylib)
release-darwin-arm64: NATIVES_SIGN=required
release-darwin-arm64: natives-darwin-arm64 | $(DIST_DIR)
ifeq ($(NATIVE_ARCH),arm64)
	$(call build_release,darwin,arm64,darwin-arm64,,)
else
	$(call require_cmd,$(ZIG))
	$(call build_release,darwin,arm64,darwin-arm64,,CC='$(CC_DARWIN_ARM64)' CXX='$(CXX_DARWIN_ARM64)' $(MACOS_SYSROOT_FLAGS))
endif
endif

## Show available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
