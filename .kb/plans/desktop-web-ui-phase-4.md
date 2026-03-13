# Implementación del Desktop Web UI para Pando (Fase 4)

## Funcionalidades Avanzadas de Pando en GUI (The Advantage)

**Objetivo:** Diferenciar el Front-end de Pando respecto al de OpenCode, sumando funcionalidades gráficas nativas exclusivas para las capacidades que Pando ofrece.

### Componentes Principales:
1. **Control Panel de Agentes (Mesnada):**
   - Una pestaña o panel que liste cada sub-agente engendrado (List Agent Spawns).
   - Ver sus logs (STDIO/STDERR interceptado y emitido por Websocket).
   - Pausarlos o controlarlos y ver cómo las "facciones" trabajan colaborativamente.

2. **Visualizador del Code of Remembrances:**
   - Explorador de bases de datos SQLite integrado (`remembrances.db`), navegable y filtrable para acceder a "Facts", "Skills" e historial de planes.
   - Herramientas visuales para buscar y editar recuerdos que formarán el LLM Context.

3. **Autenticación (Local):**
   - Interfaz gráfica para gestionar los Tool Requests e integraciones de MCP dinámicamente, con aprobación mediante la UI ("Yolo" mode toggle interactivo).

### Criterios de Finalización:
- El Desktop app supera las capacidades de OpenCode, al permitir la visibilidad completa del paradigma "Mesnada" y mostrar gráficamente el árbol de los "Remembrances Tools" integrados de forma transparente.
