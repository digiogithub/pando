# Implementación del Desktop Web UI para Pando (Fase 1)

## Servidor HTTP y API del Motor Pando (The Engine Server)

**Objetivo:** Desarrollar o habilitar un servidor HTTP embebido en Pando (`pando serve`) para proporcionar la API de respaldo que interactuará con la Interfaz Web (UI).

**Estado: COMPLETADO** ✅

### Componentes Implementados:

1. **Comando `pando serve` (cmd/serve.go):**
   - Flags: --host, --port, --debug
   - Puerto por defecto: 8765
   - Integración con app.App de Pando
   - Shutdown graceful con señales SIGINT/SIGTERM

2. **Servidor HTTP (internal/api/):**
   - Autenticación por token local
   - CORS middleware para WebUI
   - Soporte SSE para streaming

3. **Endpoints REST implementados:**
   - `GET /health` - Health check
   - `GET /api/v1/token` - Obtener token de autenticación
   - `GET /api/v1/project` - Info del proyecto actual
   - `GET /api/v1/project/context` - Contexto del proyecto
   - `GET /api/v1/sessions` - Lista de sesiones
   - `GET /api/v1/sessions/:id` - Detalle de sesión con mensajes
   - `GET /api/v1/tools` - Lista herramientas MCP disponibles
   - `GET /api/v1/files` - Navegación de archivos del proyecto
   - `GET /api/v1/files/:path` - Leer archivo específico
   - `POST /api/v1/chat` - Enviar prompt (respuesta síncrona)
   - `GET/POST /api/v1/chat/stream` - SSE streaming de respuestas LLM

4. **Streaming SSE:**
   - Conecta con `CoderAgent.Run()` para streaming
   - Eventos: session, content, done, error
   - Compatible con EventSource API del navegador

5. **Autenticación Local:**
   - Token generado al iniciar servidor
   - Header `X-Pando-Token` o query param `?token=`
   - Endpoints públicos: /health, /api/v1/token

6. **Configuración:**
   - Sección `[server]` en .pando.toml
   - Campos: enabled, host, port, requireAuth
   - Defaults: localhost:8765, requireAuth=true

### Criterios de Finalización Cumplidos:
- ✅ El servidor `pando serve` inicia una API local en puerto 8765
- ✅ Existen endpoints para devolver contexto del proyecto
- ✅ Existen endpoints para listar sesiones
- ✅ Existe endpoint SSE para enviar prompts al agente

### Archivos Creados:
- `cmd/serve.go` - Comando CLI
- `internal/api/server.go` - Servidor HTTP
- `internal/api/routes.go` - Registro de rutas
- `internal/api/handlers_base.go` - Handlers base
- `internal/api/handlers_sessions.go` - Handlers de sesiones
- `internal/api/handlers_tools.go` - Handler de tools
- `internal/api/handlers_files.go` - Handlers de archivos
- `internal/api/handlers_chat.go` - Handlers de chat/SSE

### Siguiente Paso:
Continuar con **Fase 2: Frontend SolidJS UI** - Crear la interfaz web que consuma esta API.