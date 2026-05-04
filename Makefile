# ============================================================
# Pando — top-level Makefile
# ============================================================

GOPATH ?= $(shell go env GOPATH)
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/digiogithub/pando/internal/version.Version=$(VERSION)
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
SDKROOT ?=
MACOS_MIN_VERSION ?= 11.0
CC_DARWIN_AMD64 ?= $(ZIG) cc -target x86_64-macos.$(MACOS_MIN_VERSION)
CXX_DARWIN_AMD64 ?= $(ZIG) c++ -target x86_64-macos.$(MACOS_MIN_VERSION)
CC_DARWIN_ARM64 ?= $(ZIG) cc -target aarch64-macos.$(MACOS_MIN_VERSION)
CXX_DARWIN_ARM64 ?= $(ZIG) c++ -target aarch64-macos.$(MACOS_MIN_VERSION)
MACOS_SYSROOT_FLAGS := $(if $(SDKROOT),CGO_CFLAGS="-isysroot $(SDKROOT)" CGO_CXXFLAGS="-isysroot $(SDKROOT)" CGO_LDFLAGS="-isysroot $(SDKROOT)",)

# ============================================================
# Desktop App (Wails) targets
# ============================================================

WAILS_CMD := $(shell which wails 2>/dev/null || echo "$(GOPATH)/bin/wails")
# On Ubuntu 24.04+ webkit2gtk-4.0 was replaced by webkit2gtk-4.1 — pass webkit2_41 tag.
WAILS_TAGS := $(shell pkg-config --exists webkit2gtk-4.0 2>/dev/null && echo "" || echo "webkit2_41")

.PHONY: desktop-deps desktop-ui desktop-build desktop-dev desktop-package desktop-embed desktop-clean build web-ui-embedded dist-clean release release-linux-amd64 release-linux-arm64 release-windows-amd64 release-darwin-amd64 release-darwin-arm64 help

## Install the Wails CLI (run once)
desktop-deps:
	go install github.com/wailsapp/wails/v2/cmd/wails@latest

## Build only the web-ui frontend in desktop mode (plain HTML shell for the WebView wrapper)
desktop-ui:
	@echo "Desktop uses plain HTML shell — no frontend build needed."

## Build embedded web-ui assets used by the API binary
web-ui-embedded:
	cd $(WEB_UI_DIR) && $(WEB_UI_INSTALL_CMD) && $(WEB_UI_EMBEDDED_BUILD_CMD)

## Build local CLI binary with embedded web-ui and release version from git tag
build: web-ui-embedded
	go build -ldflags '$(LDFLAGS)' -o pando .

## Full desktop build: compile pando-desktop Wails binary (requires wails CLI)
desktop-build:
	@mkdir -p internal/desktop/bin
	@[ -f internal/desktop/bin/pando-desktop ] || echo -n "" > internal/desktop/bin/pando-desktop
	cd desktop && $(WAILS_CMD) build $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),) -o pando-desktop

## Development mode: run Wails dev server with hot-reload
desktop-dev:
	cd desktop && $(WAILS_CMD) dev $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),)

## Build the pando-desktop binary and embed it into the main pando binary
## This copies the compiled binary into internal/desktop/bin/ for go:embed
desktop-embed: desktop-build
	@mkdir -p internal/desktop/bin
	@if [ -f desktop/build/bin/pando-desktop ]; then \
		cp desktop/build/bin/pando-desktop internal/desktop/bin/pando-desktop; \
	elif [ -f desktop/build/bin/pando-desktop.exe ]; then \
		cp desktop/build/bin/pando-desktop.exe internal/desktop/bin/pando-desktop; \
	elif [ -f "desktop/build/bin/pando-desktop.app/Contents/MacOS/pando-desktop" ]; then \
		cp "desktop/build/bin/pando-desktop.app/Contents/MacOS/pando-desktop" internal/desktop/bin/pando-desktop; \
	else \
		echo "ERROR: pando-desktop binary not found in desktop/build/bin/"; exit 1; \
	fi
	@echo "Embedded pando-desktop into internal/desktop/bin/pando-desktop"

## Build production packages for current platform
desktop-package:
	cd desktop && $(WAILS_CMD) build $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),) -clean -o pando-desktop

## Remove desktop build artifacts
desktop-clean:
	rm -rf desktop/build/bin
	echo -n "" > internal/desktop/bin/pando-desktop

## Remove distribution artifacts
dist-clean:
	rm -rf $(DIST_DIR)

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

define build_release
	@echo "Building $(1)/$(2)..."
	GOOS=$(1) GOARCH=$(2) CGO_ENABLED=$(CGO_ENABLED) $(5) go build -ldflags '$(LDFLAGS)' -o $(DIST_DIR)/pando-$(3)$(4) .
	if command -v $(UPX) >/dev/null 2>&1; then $(UPX) --best --lzma $(DIST_DIR)/pando-$(3)$(4); else echo "Skipping UPX for $(3)"; fi
	cd $(DIST_DIR) && zip -qm pando-$(3).zip pando-$(3)$(4)
endef

define require_cmd
	@sh -c 'command -v "$$1" >/dev/null 2>&1 || { echo "Missing required tool: $$1"; exit 1; }' -- "$(1)"
endef

## Build all release archives in dist/ for Linux, Windows and macOS
release: web-ui-embedded $(DIST_DIR) release-linux-amd64 release-linux-arm64 release-windows-amd64 $(if $(filter Darwin,$(shell uname)),release-darwin-amd64 release-darwin-arm64)

## Build Linux x64 release archive in dist/
release-linux-amd64: | $(DIST_DIR)
	$(call build_release,linux,amd64,linux-x64,)

## Build Linux arm64 release archive in dist/
release-linux-arm64: | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,linux,arm64,linux-arm64,,CC='$(CC_LINUX_ARM64)' CXX='$(CXX_LINUX_ARM64)')

## Build Windows x64 release archive in dist/
release-windows-amd64: | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,windows,amd64,windows-x64,.exe,CC='$(CC_WINDOWS_AMD64)' CXX='$(CXX_WINDOWS_AMD64)')

ifeq ($(shell uname),Darwin)
## Build macOS x64 release archive in dist/
release-darwin-amd64: | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,darwin,amd64,darwin-x64,,CC='$(CC_DARWIN_AMD64)' CXX='$(CXX_DARWIN_AMD64)' $(MACOS_SYSROOT_FLAGS))

## Build macOS arm64 release archive in dist/
release-darwin-arm64: | $(DIST_DIR)
	$(call require_cmd,$(ZIG))
	$(call build_release,darwin,arm64,darwin-arm64,,CC='$(CC_DARWIN_ARM64)' CXX='$(CXX_DARWIN_ARM64)' $(MACOS_SYSROOT_FLAGS))
endif

## Show available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
