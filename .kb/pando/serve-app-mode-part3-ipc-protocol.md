# Análisis de la Implementación del Servidor Pando — Parte 3: Protocolo IPC y Comunicación Entre Instancias

## 5. Protocolo IPC (Inter-Process Communication)

### Visión General

El protocolo IPC de Pando permite que múltiples instancias en el mismo directorio de trabajo se comuniquen entre sí. Está implementado sobre **ZMQ** (librería `go-zeromq/zmq4`) usando un patrón **PUB/SUB + ROUTER/DEALER**.

**Ficheros principales del protocolo:**
- `/www/MCP/Pando/pando/internal/ipc/bus.go` — Bus (servidor) PUB + ROUTER
- `/www/MCP/Pando/pando/internal/ipc/client.go` — Client (cliente) SUB + DEALER
- `/www/MCP/Pando/pando/internal/ipc/envelope.go` — Envelope (mensaje estándar)
- `/www/MCP/Pando/pando/internal/ipc/ports.go` — Asignación de puertos
- `/www/MCP/Pando/pando/internal/ipc/lock_common.go` — LockInfo y fichero de lock
- `/www/MCP/Pando/pando/internal/ipc/lock_unix.go` — Adquisición de lock (flock)
- `/www/MCP/Pando/pando/internal/ipc/options.go` — Configuración (timeouts)
- `/www/MCP/Pando/pando/internal/ipc/errors.go` — Definiciones de errores
- `/www/MCP/Pando/pando/internal/ipc/protocol/rpc.go` — Constantes y tipos RPC
- `/www/MCP/Pando/pando/internal/ipc/protocol/topics.go` — Constantes de topics PUB
- `/www/MCP/Pando/pando/internal/ipc/protocol/payloads.go` — Tipos de payloads
- `/www/MCP/Pando/pando/internal/ipc/bridge/bridge.go` — Bridge: conecta eventos in-process al bus
- `/www/MCP/Pando/pando/internal/ipc/bridge/handlers.go` — Handlers JSON-RPC

### Arquitectura de Sockets

**Bus (servidor — instancia primaria):**
- **PUB socket** (`tcp://127.0.0.1:{pubPort}`): Broadcasting de eventos a todas las instancias suscritas
- **ROUTER socket** (`tcp://127.0.0.1:{rpcPort}`): Atiende peticiones JSON-RPC request/response

**Client (cliente — instancias secundarias/observadoras):**
- **SUB socket**: Se conecta al PUB del Bus para recibir eventos filtrando por topic
- **DEALER socket** (cacheado): Se conecta al ROUTER del Bus para hacer llamadas RPC

### Formato de Mensajes

**Envelope (PUB):** (`envelope.go`)
```go
type Envelope struct {
    InstanceID string          `json:"instanceId"`
    ProjectID  string          `json:"projectId"`
    SessionID  string          `json:"sessionId,omitempty"`
    Topic      string          `json:"topic"`
    Timestamp  time.Time       `json:"timestamp"`
    Payload    json.RawMessage `json:"payload"`
}
```
El frame ZMQ se forma como: `[topic_bytes + 0x00 + json_envelope_bytes]`, permitiendo a los subscribers filtrar por prefijo de topic.

**JSON-RPC (ROUTER):** (`bus.go`)
```go
// Request
type rpcRequest struct {
    JSONRPC string          `json:"jsonrpc"`  // "2.0"
    ID      string          `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// Response
type rpcResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      string          `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *rpcError       `json:"error,omitempty"`
}
```

### Asignación de Puertos (`ports.go`)

- **Puertos deterministas (hash FNV-32a):** `PortsForPath(absPath)` → puerto base = `40000 + (fnv32a(path) % 20000)`, pub = base, rpc = base+1
- **Puertos libres (fallback):** `FindFreePorts()` → dos puertos TCP asignados por el SO (para instancias secundarias que no pueden usar los puertos deterministas ya ocupados por la primaria)
- **Rango de puertos base:** 40000-60000

### Lock de Instancia Primaria (`lock_unix.go`)

El mecanismo de **flock** (exclusivo por proceso) en `.pando/ipc.lock` determina qué instancia es la "primaria":
- **Primera instancia:** adquiere el lock (`LOCK_EX|LOCK_NB`), escribe su `LockInfo` (instanceID, PID, puertos), y se convierte en primaria
- **Instancias posteriores:** fallan al adquirir el lock, leen el `LockInfo` de la primaria existente y se convierten en secundarias
- Las secundarias abren la BD en modo read-only y usan DBProxy para escribir via RPC

### LockInfo (`lock_common.go`)
```go
type LockInfo struct {
    InstanceID string    `json:"instance_id"`
    PID        int       `json:"pid"`
    PubPort    int       `json:"pub_port"`
    RPCPort    int       `json:"rpc_port"`
    StartedAt  time.Time `json:"started_at"`
}
```

### Instance Registry (`instanceregistry/`)

- **Ficheros:**
  - `entry.go` — Definición de `Entry` y `Mode` (TUI, WebUI, Desktop, ACP, NonInteractive, Proxy)
  - `announce.go` — `Announce()` escribe JSON en `/tmp/pando-instances/<instanceID>.json`; `Revoke()` lo elimina
  - `registry.go` — `Registry.List()` escanea `/tmp/pando-instances/`, verifica que los PID estén vivos (`signal(0)`), y limpia entradas obsoletas
- **Propósito:** Permitir a cualquier instancia descubrir todas las demás instancias Pando en ejecución en el sistema, independientemente del directorio de trabajo

### Topics del PUB Socket (`protocol/topics.go`)

**Sesiones:**
- `session.list` — Lista completa de sesiones
- `session.update` — Sesión creada o actualizada
- `session.activated` — Sesión activa cambiada
- `session.deleted` — Sesión eliminada

**Mensajes:**
- `message.append` — Nuevo mensaje añadido

**LLM (streaming):**
- `llm.token` — Cada token streaming del LLM
- `llm.start` — Inicio de llamada LLM
- `llm.end` — Fin de llamada LLM (con tokens in/out)

**Herramientas:**
- `tool.start` — Inicio de ejecución de herramienta
- `tool.end` — Fin de ejecución de herramienta

**Instancia:**
- `instance.heartbeat` — Cada 5 segundos (liveness)
- `instance.shutdown` — Shutdown graceful

### Métodos JSON-RPC (`protocol/rpc.go`)

| Método | Parámetros | Descripción |
|--------|-----------|-------------|
| `instance.ping` | — | Verifica que la instancia está viva. Respuesta: `PingResult{Status, InstanceID, Uptime}` |
| `instance.info` | — | Información detallada |
| `session.list` | — | Lista todas las sesiones |
| `session.get` | `{session_id}` | Obtiene sesión por ID |
| `session.activate` | `{session_id}` | Cambia sesión activa (publica evento `session.activated`) |
| `message.send` | `{session_id, content}` | Envía mensaje a agente local (inicia procesamiento LLM) |
| `message.list` | `{session_id}` | Historial de mensajes de una sesión |
| `session.interrupt` | `{session_id}` | Cancela generación LLM en curso |
| `state.sync` | `{project_id}` | Solicita snapshot completo de estado |

### Bridge — Conexión de Eventos In-process al Bus (`bridge/bridge.go`)

El **Bridge** suscribe los eventos internos de las sesiones y el agente, y los re-publica en el bus ZMQ:

- `bridgeSessions()` — Suscribe eventos de `session.Service.Subscribe()` y los mapea a topics PUB:
  - `pubsub.CreatedEvent` → `session.update`
  - `pubsub.UpdatedEvent` → `session.update`
  - `pubsub.DeletedEvent` → `session.deleted`
- `bridgeAgent()` — Suscribe eventos de `agent.Service.Subscribe()` y los mapea:
  - `AgentEventTypeContentDelta` → `llm.token` (cada token)
  - `AgentEventTypeToolCall` → `tool.start`
  - `AgentEventTypeToolResult` → `tool.end` (resultado truncado a 512 chars)
  - `AgentEventTypeResponse` → `llm.end`
- `runHeartbeat()` — Publica `instance.heartbeat` cada 5 segundos con uptime y session count

### Handlers RPC del Bridge (`bridge/handlers.go`)

`RegisterHandlersWithAgent()` registra todos los métodos RPC en el Bus:
- `instance.ping` → devuelve estado, instanceID, uptime
- `session.list` → lista sesiones desde `session.Service`
- `session.get` → obtiene sesión por ID
- `session.activate` → verifica que la sesión existe, publica `session.activated`, devuelve OK
- `message.send` → ejecuta `runner.RunMessage()` (agente local) para procesar mensaje
- `session.interrupt` → ejecuta `interrupter.Cancel()` para cancelar LLM
- `message.list` → lista mensajes desde `message.Service`

`RegisterHandlers()` es una versión simplificada sin agente (runner/interrupter a nil).

### DBProxy — Proxy de Escrituras a BD (`ipc/dbproxy/`)

- **Ficheros:**
  - `/www/MCP/Pando/pando/internal/ipc/dbproxy/proxy.go` — `DBProxy` implementa `db.Querier`
  - `/www/MCP/Pando/pando/internal/ipc/dbproxy/handlers.go` — `RegisterHandlers()` para el Bus
- **Propósito:** Instancias secundarias abren la BD SQLite en modo read-only y redirigen todas las escrituras a la instancia primaria via ZMQ JSON-RPC
- **Método RPC:** `db.write` con `WriteRequest{Method, Params}`
- **Dispatcher:** `dispatchWrite()` enruta a la función `db.Querier` correspondiente (CreateSession, UpdateSession, DeleteSession, CreateMessage, UpdateMessage, DeleteMessage, CreateFile, UpdateFile, DeleteFile, InsertPromptTemplate, InsertSessionScore, InsertSkill, DeactivateLowestSkill, IncrementSkillUsage, CreateProject, UpdateProjectStatus, UpdateProjectLastOpened, MarkProjectInitialized, DeleteProject)

### Configuración del Bus en los Modos Serve/App/TUI

**En modo TUI (primaria):** (`cmd/root.go`)
```go
bus := ipc.NewBus(instanceID)
bus.Start(ctx, pubPort, rpcPort)
dbproxy.RegisterHandlers(bus, db.New(conn))     // handlers de escritura DB para secundarias
bridge.RegisterHandlers(bus, instanceID, ...)    // handlers RPC de sesiones/mensajes
pandoApp.SetupIPC(bus)                           // configura el bus como publisher de sesiones
br := bridge.New(bus, ...)                       // bridge eventos in-process → ZMQ PUB
br.Start(ctx)
```

**En modo Serve/App:** (`cmd/serve.go` y `cmd/app.go`)
- Anuncian instancia en registry (NO primarias, NO hacen lock)
- Crean Bus e inician con puertos libres
- Solo registran `bridge.RegisterHandlers()` (NO registran `dbproxy.RegisterHandlers`)
- Crean y arrancan `bridge.New()` con el CoderAgent como MessageRunner
- El bridge permite que el modo serve/app reciba mensajes entrantes via RPC y los procese con su agente local

**En modo ACP:** (`cmd/root.go` función `runACPServerWithOptions()`)
- Similar a serve/app pero con puertos deterministas (same-path collision permitida)
- Anuncia instancia como `ModeACP`
- Solo registra `bridge.RegisterHandlers()` (sin dbproxy)