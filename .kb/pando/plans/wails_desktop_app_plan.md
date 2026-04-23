# Plan: Pando Desktop App con Wails

**Fecha**: 2026-04-11  
**Estado**: Planificado  
**Objetivo**: Empaquetar Pando como aplicación de escritorio nativa usando Wails, combinando el servidor HTTP interno (`internal/api`) con la web UI React en un binario standalone multiplataforma.

---

## Análisis de la arquitectura actual

### Modos de ejecución actuales:
| Modo | Comando | Descripción |
|------|---------|-------------|
| **Comando** | `pando -p "prompt"` | No interactivo, stream a stdout, `app.RunNonInteractive()` |
| **TUI** | `pando` | Bubble Tea UI completa, suscripciones pubsub, keyboard nav |
| **Web backend** | `pando serve` | HTTP REST+SSE en puerto 8765, `internal/api.Server` |
| **ACP** | `pando --acp-server` | JSON-RPC sobre stdio para IDEs |

### Stack web-ui:
- **Frontend**: React 19 + Vite + TypeScript + TailwindCSS
- **API**: `fetch('/api/v1/...')` con URLs relativas, token en localStorage
- **Auth**: Token via `/api/v1/token`, guardado en localStorage
- **Build**: `npm run build` → `web-ui/dist/`

### Módulo Go: `github.com/digiogithub/pando`

---

## Estrategia Wails

Wails crea una ventana nativa con WebView embebido. La arquitectura desktop:
1. El proceso Wails arranca el servidor HTTP (`internal/api`) en un puerto libre aleatorio en `127.0.0.1`
2. Wails sirve el frontend React desde assets embebidos (`web-ui/dist/`)
3. Un handler custom inyecta `window.__PANDO__ = {apiBase, token}` en el HTML antes de servir
4. El frontend detecta `window.__PANDO__` y usa la URL absoluta interna + token pre-inyectado

---

## Fases de implementación

### Phase 1: Wails Setup & Scaffolding
**Fact ID**: `desktop_wails_phase1_setup`

Añadir Wails como dependencia Go y crear la estructura base:
- `go get github.com/wailsapp/wails/v2`
- Crear `desktop/` con `main.go` y `wails.json`
- Crear `internal/desktop/app.go` y `internal/desktop/embed.go`
- Assets de plataforma en `desktop/build/` (iconos, manifests)

**Archivos**: `desktop/main.go`, `desktop/wails.json`, `desktop/build/`, `internal/desktop/app.go`, `internal/desktop/embed.go`

---

### Phase 2: Backend API Server Integration
**Fact ID**: `desktop_wails_phase2_backend`

Arrancar `internal/api.Server` en puerto libre dentro del proceso Wails:
- `internal/desktop/server.go`: `StartAPIServer(ctx, cwd)` → busca puerto libre con `net.Listen("tcp","127.0.0.1:0")`
- Hook `OnStartup` en `DesktopApp` arranca el server y guarda URL+token
- Handler `inject.go` inserta `<script>window.__PANDO__={apiBase,token}</script>` en el HTML index

**Archivos**: `internal/desktop/server.go`, `internal/desktop/inject.go`, `internal/desktop/app.go` (modificado)

---

### Phase 3: Frontend Adaptation
**Fact ID**: `desktop_wails_phase3_frontend`

Adaptar `web-ui/` para modo web (URLs relativas) y modo desktop (URL absoluta + token pre-inyectado):
- `web-ui/src/services/desktop.ts`: detecta `window.__PANDO__`, exporta `isDesktop`, `desktopConfig`
- `web-ui/src/services/api.ts`: usa `window.__PANDO__?.apiBase ?? ''` como base URL, pre-carga token
- `web-ui/src/services/auth.ts`: en modo desktop, salta health check HTTP
- `web-ui/src/App.tsx`: en modo desktop, salta splash de conexión de red
- `web-ui/vite.config.ts`: añadir modo `desktop` con `base: './'`
- `web-ui/package.json`: añadir script `"build:desktop"`

**Archivos**: `web-ui/src/services/desktop.ts` (nuevo), `web-ui/src/services/api.ts`, `web-ui/src/services/auth.ts`, `web-ui/src/App.tsx`, `web-ui/vite.config.ts`, `web-ui/package.json`

---

### Phase 4: Wails Go Bindings
**Fact ID**: `desktop_wails_phase4_bindings`

Exponer funciones Go nativas al frontend TypeScript vía Wails bindings:
- `GetServerURL()`, `GetToken()`, `GetVersion()`
- Diálogos nativos: `SelectDirectory()`, `OpenFileDialog()`, `SaveFileDialog()`
- Control de ventana: `Minimize()`, `Maximize()`, `ToggleFullscreen()`, `SetTitle()`
- Sistema: `OpenInBrowser(url)`, `ShowNotification(title, body)`
- (Opcional) System tray con menu básico
- Wails auto-genera `wailsjs/go/...` TypeScript; crear wrapper `web-ui/src/services/wailsBindings.ts`

**Archivos**: `internal/desktop/bindings.go`, `internal/desktop/tray.go` (opcional), `web-ui/src/services/wailsBindings.ts` (nuevo)

---

### Phase 5: Build Pipeline & Asset Embedding
**Fact ID**: `desktop_wails_phase5_build`

Configurar pipeline completo de build:
- `Makefile` targets: `desktop-deps`, `desktop-ui`, `desktop-build`, `desktop-dev`, `desktop-package`
- `desktop/wails.json` con `frontend:build`, `frontend:dir` apuntando a `../web-ui`
- `internal/desktop/embed.go` usa `//go:embed all:frontend` (symlink a `web-ui/dist/`)
- Modo dev: `wails dev` con hot reload del frontend

**Archivos**: `Makefile` (targets añadidos), `desktop/wails.json`, `internal/desktop/embed.go`

---

### Phase 6: Packaging & Distribution
**Fact ID**: `desktop_wails_phase6_packaging`

Generar instaladores nativos multiplataforma:
- **macOS**: fat binary (arm64+amd64), `.app` bundle, DMG, code signing + notarization
- **Windows**: `.exe`, NSIS installer (`wails build -nsis`), `wails.exe.manifest`
- **Linux**: AppImage, `.deb` via `nfpm`, `.rpm` via `nfpm`
- **CI/CD**: GitHub Actions matrix (macos-latest, windows-latest, ubuntu-latest)
- **Versioning**: script `scripts/bump-version.sh` sincroniza `internal/version` + `desktop/wails.json`

**Archivos**: `desktop/build/darwin/`, `desktop/build/windows/`, `desktop/build/linux/`, `.github/workflows/desktop-build.yml`, `scripts/bump-version.sh`

---

## Árbol de archivos final

```
pando/
├── desktop/
│   ├── main.go                    # Entry point Wails
│   ├── wails.json                 # Config Wails
│   └── build/
│       ├── appicon.png
│       ├── darwin/{icon.icns, Info.plist}
│       ├── windows/{icon.ico, wails.exe.manifest}
│       └── linux/icon.png
├── internal/desktop/
│   ├── app.go                     # DesktopApp + lifecycle hooks
│   ├── bindings.go                # Bindings nativos expuestos al frontend
│   ├── server.go                  # Arranca internal/api en puerto libre
│   ├── inject.go                  # Inyecta window.__PANDO__ en HTML
│   ├── embed.go                   # //go:embed web-ui/dist
│   └── tray.go                    # System tray (opcional)
├── web-ui/src/services/
│   ├── desktop.ts                 # Detección modo desktop + tipos window.__PANDO__
│   └── wailsBindings.ts           # Re-exporta bindings Wails con fallbacks web
├── Makefile                       # Targets desktop-*
├── scripts/bump-version.sh
└── .github/workflows/desktop-build.yml
```

## Notas de implementación

- **Sin conflicto con `pando serve`**: el desktop app usa un puerto aleatorio en 127.0.0.1, nunca el 8765
- **Token seguro**: generado en startup, inyectado en HTML, no expuesto en red pública
- **Compatibilidad web**: todos los cambios en `web-ui/src/services/` son backward-compatible (fallback sin `window.__PANDO__`)
- **TUI no afectada**: `internal/tui` y `cmd/root.go` no se modifican
- **Modo comando no afectado**: `pando -p` sigue funcionando igual
