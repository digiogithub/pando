# Implementación del Desktop Web UI para Pando (Fase 2)

## Construcción de la Interfaz Web Frontend (The UI)

**Objetivo:** Crear una Single Page Application (SPA), probablemente con React o SolidJS y TailwindCSS, para que actúe como la cara gráfica de Pando.

### Componentes Principales:
1. **Configuración Inicial del Proyecto:**
   - Implementar un bundler (como Vite) en la carpeta de Pando (ej: `ui/web`).
   - Definir sistema de componentes y diseño (basado en TailwindCSS).
   
2. **Integración con Servidor (ServerGate):**
   - Emular la abstracción `ServerConnection` vista en OpenCode, con rutinas de reinicio automático y sondeo de salud (Health Check) a los endpoints del backend de Go (Pando HTTP Server).

3. **Subsistemas Web:**
   - **Área de Chat y Prompts:** Vista de burbuja fluida (SSE stream renderer).
   - **Área Editor/File Tree:** Un árbol de archivos y renderizado de *markdown* y código.
   - **Notificaciones y Preferencias:** Módulos de configuración con guardado en `localStorage`.

### Criterios de Finalización:
- La UI se compila estáticamente.
- Puede conectarse a un demonio de Pando local usando un mecanismo HTTP.
- Muestra el mismo nivel de capacidad conversacional y herramientas que el *bubbletea* del CLI.
