# ============================================================
# Pando — top-level Makefile
# ============================================================

GOPATH ?= $(shell go env GOPATH)

# ============================================================
# Desktop App (Wails) targets
# ============================================================

WAILS_CMD := $(shell which wails 2>/dev/null || echo "$(GOPATH)/bin/wails")
# On Ubuntu 24.04+ webkit2gtk-4.0 was replaced by webkit2gtk-4.1 — pass webkit2_41 tag.
WAILS_TAGS := $(shell pkg-config --exists webkit2gtk-4.0 2>/dev/null && echo "" || echo "webkit2_41")

.PHONY: desktop-deps desktop-ui desktop-build desktop-dev desktop-package desktop-clean

## Install the Wails CLI (run once)
desktop-deps:
	go install github.com/wailsapp/wails/v2/cmd/wails@latest

## Build only the web-ui frontend in desktop mode
desktop-ui:
	cd web-ui && npm install && npm run build:desktop

## Full desktop build: frontend + Go binary (requires wails CLI)
desktop-build: desktop-ui
	cd desktop && $(WAILS_CMD) build $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),)

## Development mode: hot-reload frontend + Go backend in Wails window
desktop-dev:
	cd desktop && $(WAILS_CMD) dev $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),)

## Build production packages for current platform
desktop-package: desktop-ui
	cd desktop && $(WAILS_CMD) build $(if $(WAILS_TAGS),-tags $(WAILS_TAGS),) -clean

## Remove desktop build artifacts
desktop-clean:
	rm -rf desktop/build/bin desktop/frontend

## Show available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
