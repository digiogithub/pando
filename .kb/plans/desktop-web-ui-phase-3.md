# Implementación del Desktop Web UI para Pando (Fase 3)

## Empaquetado Desktop mediante Tauri/Wails (The App)

**Objetivo:** Agrupar y empaquetar la SPA desarrollada en la Fase 2 en una aplicación nativa de escritorio (App Desktop).

### Componentes Principales:
1. **Selección del Wrapper:**
   - Opcion A: **Wails:** Mucho más directo para Pando (compilaría la UI + Go al mismo ejecutable, sin necesidad de un proceso sidecar).
   - Opción B: **Tauri (Rust) + Pando Sidecar:** Idéntica a la estructura de OpenCode. La UI de SolidJS + Tauri se comunica o invoca el proceso `pando` localmente.
   
2. **Ciclo de Vida de la App (Sidecar Management - Si es Tauri):**
   - Igual que lo que hace `src-tauri/src/cli.rs` en OpenCode, Pando Desktop iniciará un subproceso (`ChildProcess`) del servidor de Pando al iniciarse, capturará `stdout`/`stderr` y forzará la finalización al cerrarse la interfaz gráfica.

3. **Capacidades del SO:**
   - Configurar Deep Linking, Portapapeles gestionado (`@tauri-apps/plugin-clipboard-manager`), notificaciones del sistema para estado de automatización, y modo de bandeja de sistema (System Tray).

### Criterios de Finalización:
- Un binario ejectuable `.AppImage`, `.dmg` o `.exe`.
- Al abrirlo, muestra la ventana Web, inicializa en el fondo las abstracciones de Go (el agente local), y están interconectados sin intervención manual.
