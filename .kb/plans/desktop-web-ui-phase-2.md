# Desktop Web UI Implementation for Pando (Phase 2)

## Frontend Web Interface Construction (The UI)

**Goal:** Create a Single Page Application (SPA), likely using React or SolidJS with TailwindCSS, to serve as the graphical face of Pando.

### Main Components:
1. **Initial Project Setup:**
   - Implement a bundler (like Vite) in the Pando folder (e.g., `ui/web`).
   - Define component system and design (based on TailwindCSS).
   
2. **Server Integration (ServerGate):**
   - Emulate the `ServerConnection` abstraction seen in OpenCode, with auto-restart routines and health check polling against the Go backend endpoints (Pando HTTP Server).

3. **Web Subsystems:**
   - **Chat and Prompts Area:** Fluid bubble view (SSE stream renderer).
   - **Editor/File Tree Area:** A file tree and *markdown* and code rendering.
   - **Notifications and Preferences:** Configuration modules with `localStorage` persistence.

### Completion Criteria:
- The UI compiles statically.
- It can connect to a local Pando daemon using an HTTP mechanism.
- It shows the same level of conversational capability and tools as the CLI *bubbletea*.
