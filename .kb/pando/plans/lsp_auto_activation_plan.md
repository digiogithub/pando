---
created_at: 2026-06-18T09:01:18.159267523Z
updated_at: 2026-06-18T09:01:18.159267523Z
tags:
    - plan
    - lsp
    - auto-activation
    - architecture
---
# Plan: Auto-activación de LSP por tipo de fichero (estilo OpenCode)

Fecha: 2026-06-18. Proyecto: pando.

## Objetivo
1. Dejar de arrancar gopls (y cualquier LSP) de forma eager en el boot, incluso en proyectos no-Go (reminiscencia del origen Go de Pando).
2. Detectar automáticamente el lenguaje de los ficheros que se editan/abren y arrancar el language server correspondiente SOLO si su binario está instalado (exec.LookPath), de forma lazy, igual que OpenCode (`getClients`/`touchFile` con sets `broken`/`spawning`).

## Decisiones del usuario
- Servidores ya declarados en config del usuario: **lazy salvo `autostart=true`** (campo nuevo opt-in para arranque eager).
- Disparadores de `touchFile`: **los tres** → tools del agente, editor/árbol TUI, watcher del workspace.

## Estado actual (hallazgos)
- `internal/app/lsp.go:initLSPClients` arranca TODOS los `cfg.LSP` en goroutines al boot e **ignora `Disabled`** (bug a corregir).
- `internal/config/init.go:166` escribe `[LSP.gopls]` habilitado en el TOML por defecto → causa el arranque siempre de Go.
- `internal/config/lsp_presets.go::LSPPresets()` ya define 22 servidores con extensiones; hoy solo se usan en `internal/tui/page/settings.go`.
- `internal/lsp/client.go::HandlesFile` filtra por extensión; `OpenFile`/`OpenFileOnDemand` emiten didOpen.
- `app.LSPClients map[string]*lsp.Client` se pasa POR REFERENCIA a las tools (app.go:574 → agent.NewAgent). Insertar en runtime es visible, PERO las tools iteran el mapa sin lock (race latente con la inserción concurrente bajo `clientsMutex`).
- Tools que usan el mapa: edit.go, write.go, patch.go, view.go, diagnostics.go (helpers `notifyLspOpenFile`/`waitForLspDiagnostics`/`getDiagnostics`).

## Diseño
### Registry de auto-activación
Merge de `LSPPresets()` + entradas de usuario `cfg.LSP` (usuario sobrescribe preset por nombre y puede añadir nuevos) → mapa extensión→[]serverSpec. serverSpec = {Name, Command, Args, Languages, Disabled, Autostart}.

### Manager lazy en App
Añadir a App: `lspBroken map[string]struct{}` (binario ausente o fallo de init) y `lspSpawning map[string]struct{}` (en vuelo, dedupe), bajo `clientsMutex`.
`EnsureLSPForFile(ctx, path)`:
1. ext := filepath.Ext(path).
2. Para cada spec cuyas Languages contengan ext (o vacío): si Disabled→skip; si ya en LSPClients/broken/spawning→skip; marca spawning.
3. `exec.LookPath(Command)`; si falla→broken + log debug "binary not found", desmarca spawning.
4. `go createAndStartLSPClient(...)`; al terminar desmarca spawning; si init falla→broken.
Spawn es asíncrono (init hasta 30s); la primera vez los diagnósticos llegan en ediciones posteriores (igual que OpenCode). La ventana de 5s de `waitForLspDiagnostics` captura servers rápidos.

### Interfaz LSPProvider (arregla el race + cablea lazy)
Sustituir el mapa crudo en las tools por:
```go
type LSPProvider interface {
    EnsureForFile(ctx context.Context, path string)
    ClientsForFile(path string) map[string]*lsp.Client // copia bajo lock
    Clients() map[string]*lsp.Client                   // copia bajo lock
}
```
App implementa la interfaz con snapshots bajo `clientsMutex`. Los helpers de diagnostics.go siguen operando sobre el snapshot.

### Config
- `LSPConfig.Autostart bool` (json `autostart`).
- Toggle global auto-activación (default ON) — sección/campo nuevo (no puede ir como clave escalar dentro del map `[LSP]`).
- Detección binario: `exec.LookPath`; si ruta absoluta, `os.Stat`.

## Fases
**F1 — Registry & modelo de config**: añadir `Autostart`, toggle global, función registry (merge presets+usuario, override por nombre). Tests de matching por extensión y override.
**F2 — Manager lazy en App**: sets broken/spawning, `EnsureLSPForFile` (LookPath gating, dedupe, spawn async, bookkeeping), implementar `LSPProvider` con snapshots. Tests con lookPath inyectado + fake spawn.
**F3 — Quitar gopls eager**: init.go default TOML sin gopls habilitado (+ comentario auto-detect); `initLSPClients` solo eager para `Autostart && !Disabled`, resto lazy; honrar `Disabled` (fix bug). Nota de migración.
**F4 — Triggers en tools del agente**: cambiar constructores edit/write/patch/view/diagnostics + agent/tools.go + agent.go a `LSPProvider`; EnsureForFile→ClientsForFile snapshot; app.go pasa `app`. `go test ./internal/llm/agent ./internal/llm/tools`.
**F5 — Triggers TUI + watcher**: editor/file-tree → cmd a `app.EnsureLSPForFile`; hook EnsureForFile en eventos del watcher + watcher de arranque ligero cuando AutoActivate y 0 clientes; settings page muestra estado instalado/no-instalado/broken por preset.
**F6 — Docs/KB/tests**: README + doc de feature en KB, esquema config, run final de tests y verificación manual (abrir .py en proyecto no-Go arranca pyright/pylsp si está; proyecto Go ya no arranca gopls hasta tocar un .go).

## Verificación
`go test ./internal/lsp/... ./internal/llm/agent ./internal/llm/tools ./internal/config`
Manual: proyecto no-Go no arranca gopls; tocar fichero de un lenguaje con binario instalado arranca su LSP; sin binario marca broken y no reintenta en bucle.

## Riesgos
- Race del mapa: resuelto migrando tools a snapshots vía LSPProvider.
- Watcher bootstrap (sin cliente no hay watcher): mitigado con watcher ligero opcional cuando AutoActivate y 0 clientes.
- Compat: usuarios con `[LSP.gopls]` propio pasan a lazy; documentar `autostart=true` para recuperar eager.
