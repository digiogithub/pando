# Análisis de la Implementación del Servidor Pando (modos "serve" y "app") — Parte 1: Visión General y Servidor API

## 1. Introducción

Pando puede ejecutarse en varios modos, siendo los modos **"serve"** y **"app"** los que exponen un servidor HTTP REST API. El modo **"app"** es una variante del modo **"serve"** que además sirve la interfaz web embebida (WebUI) desde el mismo puerto y abre automáticamente el navegador al iniciar.

---

## 2. Punto de Entrada: Comandos CLI

### `cmd/serve.go`
- **Fichero:** `/www/MCP/Pando/pando/cmd/serve.go`
- Define el subcomando `pando serve`
- **Flags:**
  - `--host` (default: `localhost`) — interfaz de red a la que vincularse
  - `--port` (default: `8765`) — puerto preferido
  - `--debug` — logging verbose
  - `--tls-cert`, `--tls-key` — certificados TLS (auto-generados si se omiten)
- **Flujo de inicio:**
  1. Elige puerto disponible con `chooseAvailablePort()` (prueba hasta 10 puertos secuenciales desde el preferido, luego uno aleatorio)
  2. Carga la configuración con `config.Load(cwd, debug, "")`
  3. Conecta la base de datos SQLite con `db.Connect()`
  4. Resuelve certificados TLS (usa `tlsutil.EnsureCert()` si no se proporcionan)
  5. Crea el `api.Server` con `api.NewServer(ctx, cfg)` — **NO** incluye `StaticFS` ni `OpenUI`
  6. Inicializa el **IPC**: anuncia la instancia en el `instanceregistry`, crea un `ipc.Bus`, inicia el bus ZMQ, registra los handlers del bridge, inicia el bridge
  7. Espera señales de shutdown (SIGINT/SIGTERM) con watchdog de 6 segundos
  8. Inicia el servidor HTTP/TLS con `server.Start()`

### `cmd/app_command.go` y `cmd/app.go`
- **Ficheros:**
  - `/www/MCP/Pando/pando/cmd/app_command.go` — define el subcomando `pando app`
  - `/www/MCP/Pando/pando/cmd/app.go` — contiene `runAppMode()`
- **Flags:** idénticos a `serve`
- **Diferencia clave con `serve`:** Incluye `StaticFS` (WebUI embebida) y `OpenUI: true`
- **Flujo de inicio (adicional respecto a serve):**
  1. Carga la WebUI embebida con `api.EmbeddedWebUI()` (usa `embed.FS` desde `webui/dist/`)
  2. Pasa `StaticFS` y `OpenUI: true` a `api.NewServer()`
  3. Tras 350ms, abre el navegador automáticamente con `auth.OpenBrowser(baseURL)`

### `cmd/root.go` — modo TUI (contexto IPC relevante)
- **Fichero:** `/www/MCP/Pando/pando/cmd/root.go`
- El modo TUI (`pando` sin subcomando) también integra IPC completo incluyendo:
  - **Adquisición de primacía** mediante `ipc.AcquireLock()` (flock exclusivo en `.pando/ipc.lock`)
  - **DBProxy** para instancias secundarias que redirigen escrituras a la primaria vía ZMQ
  - Si es primaria: inicia el bus ZMQ, registra handlers de `dbproxy` y `bridge`, llama a `app.SetupIPC(bus)`

---

## 3. Servidor HTTP API

### `internal/api/server.go`
- **Fichero:** `/www/MCP/Pando/pando/internal/api/server.go`
- **Estructura `Server`:**
  ```go
  type Server struct {
      httpServer    *http.Server
      app           *app.App
      config        ServerConfig
      token         string              // token de autenticación generado aleatoriamente
      staticFS      fs.FS               // WebUI embebida (solo en modo "app")
      staticHandler http.Handler        // file server para WebUI
      bgRunner      *BackgroundSessionManager  // gestor de sesiones en background
  }
  ```
- **`ServerConfig`:**
  ```go
  type ServerConfig struct {
      Host, Port, Version string/int
      DB                  *sql.DB
      CWD                 string
      StaticFS            fs.FS      // nil en modo "serve", presente en modo "app"
      OpenUI              bool       // true en modo "app"
      UIBaseURL           string
      TLSCertFile, TLSKeyFile string
  }
  ```
- **`NewServer()`:**
  1. Crea `app.New()` (aplicación principal con todos los servicios)
  2. Activa auto-aprobación global de permisos
  3. Genera token de autenticación aleatorio (32 caracteres alfanuméricos)
  4. Crea `BackgroundSessionManager`
  5. Configura el router con `registerRoutes(mux)`
  6. Envuelve el router con middleware: `corsMiddleware(authMiddleware(mux))` o si hay WebUI: `corsMiddleware(uiHandler(authMiddleware(mux)))`
  7. Configura `http.Server` con timeouts (ReadTimeout: 30s, WriteTimeout: 0)

### Middleware
- **`corsMiddleware`**: Permite CORS desde cualquier origen (`*`), métodos GET/POST/PUT/PATCH/DELETE/OPTIONS, headers `Content-Type` y `X-Pando-Token`
- **`authMiddleware`**: Requiere `X-Pando-Token` para rutas `/api/...` (excepto `/health` y `/api/v1/token`)
- **`uiHandler`**: Sirve assets estáticos de la WebUI, redirige rutas no-API al `index.html` (SPA routing); soporta compresión brotli/gzip

### Autenticación
- Token aleatorio de 32 caracteres generado en `generateToken()` (Nótese: no usa criptografía segura, solo distribución modulo-length)
- Se obtiene via `GET /api/v1/token` (sin autenticación)
- Se envía en header `X-Pando-Token` o query param `token`

### WebUI Embebida
- **Fichero:** `internal/api/ui_assets_app.go`
- Usa `//go:embed webui/dist/**` para incrustar la WebUI en el binario
- Soporta compresión brotli (`.br`) y gzip (`.gz`) para assets pre-comprimidos
- Inyecta `window.__PANDO_API_BASE__` en el HTML para configurar la URL base de la API desde el servidor
