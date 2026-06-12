# Analysis of Pando Server Implementation ("serve" and "app" modes) — Part 1: Overview and API Server

## 1. Introduction

Pando can run in several modes, with the **"serve"** and **"app"** modes being those that expose an HTTP REST API server. The **"app"** mode is a variant of the **"serve"** mode that also serves the embedded web interface (WebUI) from the same port and automatically opens the browser on startup.

---

## 2. Entry Point: CLI Commands

### `cmd/serve.go`
- **File:** `/www/MCP/Pando/pando/cmd/serve.go`
- Defines the `pando serve` subcommand
- **Flags:**
  - `--host` (default: `localhost`) — network interface to bind to
  - `--port` (default: `8765`) — preferred port
  - `--debug` — verbose logging
  - `--tls-cert`, `--tls-key` — TLS certificates (auto-generated if omitted)
- **Startup flow:**
  1. Choose available port with `chooseAvailablePort()` (tries up to 10 sequential ports from the preferred one, then a random one)
  2. Load configuration with `config.Load(cwd, debug, "")`
  3. Connect SQLite database with `db.Connect()`
  4. Resolve TLS certificates (uses `tlsutil.EnsureCert()` if not provided)
  5. Create `api.Server` with `api.NewServer(ctx, cfg)` — does **NOT** include `StaticFS` or `OpenUI`
  6. Initialize **IPC**: announce instance in `instanceregistry`, create an `ipc.Bus`, start ZMQ bus, register bridge handlers, start bridge
  7. Wait for shutdown signals (SIGINT/SIGTERM) with 6-second watchdog
  8. Start HTTP/TLS server with `server.Start()`

### `cmd/app_command.go` and `cmd/app.go`
- **Files:**
  - `/www/MCP/Pando/pando/cmd/app_command.go` — defines the `pando app` subcommand
  - `/www/MCP/Pando/pando/cmd/app.go` — contains `runAppMode()`
- **Flags:** identical to `serve`
- **Key difference from `serve`:** Includes `StaticFS` (embedded WebUI) and `OpenUI: true`
- **Startup flow (additional vs serve):**
  1. Load embedded WebUI with `api.EmbeddedWebUI()` (uses `embed.FS` from `webui/dist/`)
  2. Pass `StaticFS` and `OpenUI: true` to `api.NewServer()`
  3. After 350ms, automatically open browser with `auth.OpenBrowser(baseURL)`

### `cmd/root.go` — TUI mode (relevant IPC context)
- **File:** `/www/MCP/Pando/pando/cmd/root.go`
- TUI mode (`pando` without subcommand) also integrates full IPC including:
  - **Primacy acquisition** via `ipc.AcquireLock()` (exclusive flock on `.pando/ipc.lock`)
  - **DBProxy** for secondary instances that redirect writes to the primary via ZMQ
  - If primary: start ZMQ bus, register `dbproxy` and `bridge` handlers, call `app.SetupIPC(bus)`

---

## 3. HTTP API Server

### `internal/api/server.go`
- **File:** `/www/MCP/Pando/pando/internal/api/server.go`
- **`Server` struct:**
  ```go
  type Server struct {
      httpServer    *http.Server
      app           *app.App
      config        ServerConfig
      token         string              // randomly generated authentication token
      staticFS      fs.FS               // embedded WebUI (only in "app" mode)
      staticHandler http.Handler        // file server for WebUI
      bgRunner      *BackgroundSessionManager  // background session manager
  }
  ```
- **`ServerConfig`:**
  ```go
  type ServerConfig struct {
      Host, Port, Version string/int
      DB                  *sql.DB
      CWD                 string
      StaticFS            fs.FS      // nil in "serve" mode, present in "app" mode
      OpenUI              bool       // true in "app" mode
      UIBaseURL           string
      TLSCertFile, TLSKeyFile string
  }
  ```
- **`NewServer()`:**
  1. Create `app.New()` (main application with all services)
  2. Enable global permission auto-approval
  3. Generate random authentication token (32 alphanumeric characters)
  4. Create `BackgroundSessionManager`
  5. Configure router with `registerRoutes(mux)`
  6. Wrap router with middleware: `corsMiddleware(authMiddleware(mux))` or if WebUI: `corsMiddleware(uiHandler(authMiddleware(mux)))`
  7. Configure `http.Server` with timeouts (ReadTimeout: 30s, WriteTimeout: 0)

### Middleware
- **`corsMiddleware`**: Allows CORS from any origin (`*`), methods GET/POST/PUT/PATCH/DELETE/OPTIONS, headers `Content-Type` and `X-Pando-Token`
- **`authMiddleware`**: Requires `X-Pando-Token` for `/api/...` routes (except `/health` and `/api/v1/token`)
- **`uiHandler`**: Serves WebUI static assets, redirects non-API routes to `index.html` (SPA routing); supports brotli/gzip compression

### Authentication
- Random 32-character token generated in `generateToken()` (Note: does not use secure cryptography, only modulo-length distribution)
- Obtained via `GET /api/v1/token` (no authentication required)
- Sent in header `X-Pando-Token` or query param `token`

### Embedded WebUI
- **File:** `internal/api/ui_assets_app.go`
- Uses `//go:embed webui/dist/**` to embed the WebUI in the binary
- Supports brotli (`.br`) and gzip (`.gz`) compression for pre-compressed assets
- Injects `window.__PANDO_API_BASE__` in the HTML to configure the API base URL from the server