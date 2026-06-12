# Plan: Pando Desktop App with Wails

**Date**: 2026-04-11  
**Status**: Planned  
**Objective**: Package Pando as a native desktop application using Wails, combining the internal HTTP server (`internal/api`) with the React web UI into a standalone cross-platform binary.

---

## Current Architecture Analysis

### Current execution modes:
| Mode | Command | Description |
|------|---------|-------------|
| **Command** | `pando -p "prompt"` | Non-interactive, stream to stdout, `app.RunNonInteractive()` |
| **TUI** | `pando` | Full Bubble Tea UI, pubsub subscriptions, keyboard nav |
| **Web backend** | `pando serve` | HTTP REST+SSE on port 8765, `internal/api.Server` |
| **ACP** | `pando --acp-server` | JSON-RPC over stdio for IDEs |

### web-ui stack:
- **Frontend**: React 19 + Vite + TypeScript + TailwindCSS
- **API**: `fetch('/api/v1/...')` with relative URLs, token in localStorage
- **Auth**: Token via `/api/v1/token`, stored in localStorage
- **Build**: `npm run build` → `web-ui/dist/`

### Go module: `github.com/digiogithub/pando`

---

## Wails Strategy

Wails creates a native window with embedded WebView. The desktop architecture:
1. The Wails process starts the HTTP server (`internal/api`) on a random free port on `127.0.0.1`
2. Wails serves the React frontend from embedded assets (`web-ui/dist/`)
3. A custom handler injects `window.__PANDO__ = {apiBase, token}` into the HTML before serving
4. The frontend detects `window.__PANDO__` and uses the absolute internal URL + pre-injected token

---

## Implementation Phases

### Phase 1: Wails Setup & Scaffolding
**Fact ID**: `desktop_wails_phase1_setup`

Add Wails as a Go dependency and create the base structure:
- `go get github.com/wailsapp/wails/v2`
- Create `desktop/` with `main.go` and `wails.json`
- Create `internal/desktop/app.go` and `internal/desktop/embed.go`
- Platform assets in `desktop/build/` (icons, manifests)

**Files**: `desktop/main.go`, `desktop/wails.json`, `desktop/build/`, `internal/desktop/app.go`, `internal/desktop/embed.go`

---

### Phase 2: Backend API Server Integration
**Fact ID**: `desktop_wails_phase2_backend`

Start `internal/api.Server` on a free port within the Wails process:
- `internal/desktop/server.go`: `StartAPIServer(ctx, cwd)` → find free port with `net.Listen("tcp","127.0.0.1:0")`
- `OnStartup` hook in `DesktopApp` starts the server and stores URL+token
- `inject.go` handler inserts `<script>window.__PANDO__={apiBase,token}</script>` into the index HTML

**Files**: `internal/desktop/server.go`, `internal/desktop/inject.go`, `internal/desktop/app.go` (modified)

---

### Phase 3: Frontend Adaptation
**Fact ID**: `desktop_wails_phase3_frontend`

Adapt `web-ui/` for web mode (relative URLs) and desktop mode (absolute URL + pre-injected token):
- `web-ui/src/services/desktop.ts`: detects `window.__PANDO__`, exports `isDesktop`, `desktopConfig`
- `web-ui/src/services/api.ts`: uses `window.__PANDO__?.apiBase ?? ''` as base URL, pre-loads token
- `web-ui/src/services/auth.ts`: in desktop mode, skips HTTP health check
- `web-ui/src/App.tsx`: in desktop mode, skips network connection splash
- `web-ui/vite.config.ts`: add `desktop` mode with `base: './'`
- `web-ui/package.json`: add `"build:desktop"` script

**Files**: `web-ui/src/services/desktop.ts` (new), `web-ui/src/services/api.ts`, `web-ui/src/services/auth.ts`, `web-ui/src/App.tsx`, `web-ui/vite.config.ts`, `web-ui/package.json`

---

### Phase 4: Wails Go Bindings
**Fact ID**: `desktop_wails_phase4_bindings`

Expose native Go functions to the TypeScript frontend via Wails bindings:
- `GetServerURL()`, `GetToken()`, `GetVersion()`
- Native dialogs: `SelectDirectory()`, `OpenFileDialog()`, `SaveFileDialog()`
- Window control: `Minimize()`, `Maximize()`, `ToggleFullscreen()`, `SetTitle()`
- System: `OpenInBrowser(url)`, `ShowNotification(title, body)`
- (Optional) System tray with basic menu
- Wails auto-generates `wailsjs/go/...` TypeScript; create wrapper `web-ui/src/services/wailsBindings.ts`

**Files**: `internal/desktop/bindings.go`, `internal/desktop/tray.go` (optional), `web-ui/src/services/wailsBindings.ts` (new)

---

### Phase 5: Build Pipeline & Asset Embedding
**Fact ID**: `desktop_wails_phase5_build`

Configure complete build pipeline:
- `Makefile` targets: `desktop-deps`, `desktop-ui`, `desktop-build`, `desktop-dev`, `desktop-package`
- `desktop/wails.json` with `frontend:build`, `frontend:dir` pointing to `../web-ui`
- `internal/desktop/embed.go` uses `//go:embed all:frontend` (symlink to `web-ui/dist/`)
- Dev mode: `wails dev` with frontend hot reload

**Files**: `Makefile` (targets added), `desktop/wails.json`, `internal/desktop/embed.go`

---

### Phase 6: Packaging & Distribution
**Fact ID**: `desktop_wails_phase6_packaging`

Generate native cross-platform installers:
- **macOS**: fat binary (arm64+amd64), `.app` bundle, DMG, code signing + notarization
- **Windows**: `.exe`, NSIS installer (`wails build -nsis`), `wails.exe.manifest`
- **Linux**: AppImage, `.deb` via `nfpm`, `.rpm` via `nfpm`
- **CI/CD**: GitHub Actions matrix (macos-latest, windows-latest, ubuntu-latest)
- **Versioning**: `scripts/bump-version.sh` script synchronizes `internal/version` + `desktop/wails.json`

**Files**: `desktop/build/darwin/`, `desktop/build/windows/`, `desktop/build/linux/`, `.github/workflows/desktop-build.yml`, `scripts/bump-version.sh`

---

## Final File Tree

```
pando/
├── desktop/
│   ├── main.go                    # Wails entry point
│   ├── wails.json                 # Wails config
│   └── build/
│       ├── appicon.png
│       ├── darwin/{icon.icns, Info.plist}
│       ├── windows/{icon.ico, wails.exe.manifest}
│       └── linux/icon.png
├── internal/desktop/
│   ├── app.go                     # DesktopApp + lifecycle hooks
│   ├── bindings.go                # Native bindings exposed to frontend
│   ├── server.go                  # Starts internal/api on free port
│   ├── inject.go                  # Injects window.__PANDO__ into HTML
│   ├── embed.go                   # //go:embed web-ui/dist
│   └── tray.go                    # System tray (optional)
├── web-ui/src/services/
│   ├── desktop.ts                 # Desktop mode detection + window.__PANDO__ types
│   └── wailsBindings.ts           # Re-exports Wails bindings with web fallbacks
├── Makefile                       # desktop-* targets
├── scripts/bump-version.sh
└── .github/workflows/desktop-build.yml
```

## Implementation Notes

- **No conflict with `pando serve`**: the desktop app uses a random port on 127.0.0.1, never 8765
- **Secure token**: generated at startup, injected into HTML, not exposed on public network
- **Web compatibility**: all changes in `web-ui/src/services/` are backward-compatible (fallback without `window.__PANDO__`)
- **TUI unaffected**: `internal/tui` and `cmd/root.go` are not modified
- **Command mode unaffected**: `pando -p` continues to work the same way
