# Implementación del Desktop Web UI para Pando (Fase 1)

## Servidor HTTP y API del Motor Pando (The Engine Server)

**Objetivo:** Desarrollar o habilitar un servidor HTTP embebido en Pando (`pando serve` o `pando acp start --transport http`) para proporcionar la API de respaldo que interactuará con la Interfaz Web (UI).

### Componentes Principales:
1. **Gestión de Rutas HTTP (Routers):**
   - Configurar un multiplexor (por ejemplo, con `chi` o el paquete estándar `net/http` de Go).
   - Definir rutas REST: `/api/files` (gestión de archivos), `/api/sessions` (historico de burbujas/chat), `/api/tools` (herramientas MCP disponibles).
   
2. **Streaming a través de SSE (Server-Sent Events) o WebSockets:**
   - La respuesta del LLM debe retransmitirse a la UI de forma fluida. Pando deberá implementar manejadores SSE para emitir flujos de texto, al igual que hace con la UI actual de *bubbletea*.

3. **Autenticación (Local):**
   - Agregar medidas básicas en caso de conectar remoto (paso de token en requests locales), utilizando los esquemas de la actual `auth.go`.

### Criterios de Finalización:
- El servidor `pando serve` inicia una API local en un puerto definido (ej: 8765).
- Existen endpoints capaces de devolver contexto del proyecto, listas de sesiones y enviar prompts al agente por SSE.
