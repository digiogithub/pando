# Desktop Web UI Implementation for Pando (Phase 3)

## Desktop Packaging via Tauri/Wails (The App)

**Objective:** Bundle and package the SPA developed in Phase 2 into a native desktop application (Desktop App).

### Main Components:
1. **Wrapper Selection:**
   - Option A: **Wails:** Much more straightforward for Pando (would compile the UI + Go into the same executable, without needing a sidecar process).
   - Option B: **Tauri (Rust) + Pando Sidecar:** Identical to the OpenCode structure. The SolidJS + Tauri UI communicates with or invokes the local `pando` process.
    
2. **App Lifecycle (Sidecar Management - If using Tauri):**
   - Similar to what `src-tauri/src/cli.rs` does in OpenCode, Pando Desktop will start a child process (`ChildProcess`) of the Pando server at startup, capture `stdout`/`stderr`, and force termination when the GUI is closed.

3. **OS Capabilities:**
   - Configure Deep Linking, managed Clipboard (`@tauri-apps/plugin-clipboard-manager`), system notifications for automation status, and System Tray mode.

### Completion Criteria:
- An executable binary `.AppImage`, `.dmg`, or `.exe`.
- When opened, it displays the Web window, initializes the Go abstractions (local agent) in the background, and they are interconnected without manual intervention.
