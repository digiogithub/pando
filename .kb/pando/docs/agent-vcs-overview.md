# Agent-VCS — Mini-jj para Pando

## Qué es

Agent-VCS es un sistema de control de versiones ligero inspirado en Jujutsu (jj) que reemplaza el antiguo sistema de snapshots de Pando. Registra automáticamente los cambios en ficheros del working directory a lo largo de cada sesión de agente, formando una cadena lineal de commits inmutables por sesión.

## Objetivo

Tener un histórico o bitácora de cambios por sesión en los ficheros del proyecto. Cada sesión de agente (conversación) produce una secuencia de commits que permite revisar qué cambió, cuándo, y revertir a cualquier punto.

## Modelo de datos

- **Commit**: inmutable, identificado por hash de contenido. Contiene referencia al tree, parent, session ID, descripción, timestamp, stats de ficheros.
- **Tree**: lista ordenada de TreeEntry (path, hash SHA-256, size, modtime). Almacenado por hash — si dos commits tienen el mismo árbol, se deduplica.
- **TreeEntry**: un fichero o directorio dentro de un Tree.
- **Blob**: contenido de fichero comprimido con gzip, almacenado por hash SHA-256 (content-addressable). Se deduplica entre commits y sesiones.
- **SessionLog**: lista ordenada de commit IDs para una sesión.
- **DiffEntry**: cambio a nivel de fichero entre dos commits (added/modified/deleted).
- **CommitSummary**: vista ligera de un commit para listados (incluye short ID y count de ficheros cambiados).

## Flujo por sesión

1. **Session Start** → `NewChange(sessionID, description)` — crea el commit raíz con un tree completo del working directory.
2. **Durante la sesión** (agent loop) → `Record(sessionID, description)` — compara el tree actual con el último commit; si el treeID cambió, crea un nuevo commit delta. Si no hay cambios, no hace nada.
3. **Session End** → `Record(sessionID, "Session end: ...")` — commit final de la sesión.

La integración usa el Adapter que implementa la interfaz `session.SnapshotCreator`, mapeando "start" a NewChange y el resto a Record.

## Almacenamiento en disco

```
{data-dir}/agent-vcs/
├── commits/{commitID}.json     # Commit inmutable (JSON)
├── trees/{treeID}.json         # Tree snapshot (JSON)
├── blobs/{h[0:2]}/{h[2:4]}/{h} # Blobs gzip content-addressable
└── sessions/{sessionID}.json   # Cadena de commits por sesión
```

Se guarda junto a donde se guardaban los snapshots, bajo el directorio de datos configurado.

## Soporte de ficheros ignorados

- Lee `.gitignore` y `.pandoignore` desde el directorio raíz hasta la raíz del filesystem.
- Siempre ignora `.git/` y `.pando/`.
- Aplica los `excludePatterns` del config de snapshots (ej: `node_modules/`, `*.log`, `vendor/`).
- Respeta el límite de tamaño máximo de fichero (`maxFileSize` del config).
- Solo escanea directorios que sean proyectos reconocidos (con marcadores como `.git`, `go.mod`, etc.).

## Ficheros del paquete `internal/agentvcs/`

| Fichero | Rol |
|---------|-----|
| `model.go` | Structs (Commit, Tree, TreeEntry, SessionLog, DiffEntry, CommitSummary), funciones de hash (computeCommitID, computeTreeID) |
| `storage.go` | Persistencia en disco: CRUD de commits, trees, blobs (gzip), session logs. Content-addressable con deduplicación. |
| `scanner.go` | Escaneo del working directory con soporte .gitignore/.pandoignore y exclude patterns del config |
| `service.go` | Servicio principal: NewChange, Record, Log, Diff, DiffFromParent, Revert, Cleanup, ListSessions |
| `adapter.go` | Adapter que implementa `session.SnapshotCreator` para integrar con el ciclo de vida de sesiones |
| `agentvcs_test.go` | Tests unitarios: storage roundtrip, tree dedup, blob roundtrip, diff, scanner ignore, cleanup |

## Ficheros modificados en la integración

| Fichero | Cambio |
|---------|--------|
| `internal/app/app.go` | Campo `AgentVCS` reemplaza `Snapshots`. Inicialización con `agentvcs.NewService()`. Cleanup en shutdown. |
| `cmd/root.go` | Subscriber de pubsub cambiado a `app.AgentVCS` |
| `cmd/agentvcs.go` | **Nuevo**: 5 subcomandos CLI (sessions, log, show, revert, compact) |
| `internal/api/handlers_snapshots.go` | Reescrito para delegar a AgentVCS manteniendo API backward-compatible |
| `internal/api/handlers_agentvcs.go` | **Nuevo**: endpoints nativos (sessions, log, commit, diff) |
| `internal/api/handlers_extras.go` | `/snapshots/count` cuenta commits via AgentVCS |
| `internal/api/routes.go` | Registra 4 endpoints nuevos bajo `/api/v1/agentvcs/` |
| `internal/tui/tui.go` | Comando manual usa `AgentVCS.Record` |
| `internal/tui/page/snapshots.go` | Carga commits desde `AgentVCS.Log()` |
| `internal/tui/components/snapshots/table.go` | Usa `agentvcs.Commit` en los eventos pubsub |
| `internal/tui/components/snapshots/details.go` | Actualizado para el nuevo modelo |

## Comandos CLI

```
pando agent-vcs sessions              # Lista sesiones con commits
pando agent-vcs log <session-id>      # Cadena de commits de una sesión
pando agent-vcs show <commit-id>      # Detalle + diff de un commit
pando agent-vcs revert <commit-id>    # Revierte working dir a un commit (con safety backup)
pando agent-vcs compact --keep N      # Compacta: mantiene solo las N sesiones más recientes
```

Alias disponible: `pando avcs <command>`.

## API REST

Endpoints backward-compatible bajo `/api/v1/snapshots/` (delegando a AgentVCS) más endpoints nativos:

```
GET  /api/v1/agentvcs/sessions              # Lista sesiones
GET  /api/v1/agentvcs/sessions/{id}/log     # Log de commits por sesión
GET  /api/v1/agentvcs/commits/{id}          # Detalle de commit + diff
GET  /api/v1/agentvcs/commits/{id}/diff     # Solo diff de un commit
```

## Compactación y limpieza

- **Automática** al cerrar la app: usa `cfg.Snapshots.AutoCleanupDays` y `cfg.Snapshots.MaxSnapshots` (default 100 sesiones, 30 días).
- **Manual** via CLI: `pando avcs compact --keep 50 --days 30`.
- Las sesiones se ordenan por fecha de última actualización (newest-first); se podan las que exceden el límite.
- Tras podar sesiones, se eliminan blobs huérfanos no referenciados por ningún tree.

## Configuración

Reutiliza la sección `snapshots` del config de Pando:

```toml
[snapshots]
enabled = true
maxSnapshots = 100        # Máx sesiones a mantener
maxFileSize = "10MB"      # Tamaño máximo por fichero
excludePatterns = ["node_modules/", ".git/", "vendor/", "*.log", "*.tmp"]
autoCleanupDays = 30      # Días antes de podar sesiones antiguas
```
