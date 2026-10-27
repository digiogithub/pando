# Native desktop P2: settings (MyGo)

Date: 2026-10-27. Workspace /www/MCP/Pando/pando-native-desktop (not committed). Plan: [[native_desktop_mygo]].

## What
Area `settings` (`internal/nativeui/app/settings`, route `/settings/{section...}`, also `?section=`), all 23 Web UI categories plus extension sections.
- Engine (`form.go`, `specs.go`): data-driven `Doc` over a JSON endpoint (`client.Obj`, numbers kept as json.Number so unknown keys round-trip); `Sub`/`PutWhole` for the services doc (snapshots, server, mesnada, remembrances) and sandbox (`config`). Field kinds: bool/text/int/float/select/list/secret/static/slider; Inverse, Empty, Enabled/Visible hooks. Used by general, tools, bash, token-optimization, sandbox, container, mcp-gateway, snapshots, api-server (+regenerate token), lua, self-improvement, appearance (theme staged as `family-mode`).
- Custom UIs: providers (accounts CRUD, Copilot login), agents, model-auto-mode (+playground), decision-model, mcp-servers (tools reload, OAuth), lsp (presets, activation), skills, design-system, mesnada (engine toggles, nesting cap), remembrances (embedding models, reindex, projects), webui-access (basic auth users). Helpers in `crud.go`.
- General includes `desktop_tray_on_minimize_close`, language (stored via /api/v1/ui/preferences), version, telemetry id regenerate.
- Unsaved guard: `app.LeaveGuard` (new `app/guard.go`, small hook in `layout.go navItem`) + dialog Save/Discard/Cancel on category switch and area leave.
- UI policy (`/api/v1/config/ui-policy`) hides categories; banner shown. Extension settings panels (ES modules) listed in nav with an "Open in browser" placeholder (needs webview, P4).
- Client: `client/settings*.go` (Obj helpers, paths, UI prefs, policy, typed DTOs per section).

## Verification
`go vet ./internal/nativeui/...`, `go test ./internal/nativeui/app/settings ./internal/nativeui/client` pass (every category builds, PUT shapes, unknown keys/large numbers kept, guard dialog, per-section fake-server tests). Real app (isolated HOME, `PANDO_NATIVE_PATH=/settings/<id>`): screenshots in scratchpad/screens/settings-*.png.

## Gaps
Model pickers are plain selects (no combobox); no UI-scale setting; theme not re-applied at app start (only when Appearance/General is opened); accent local only; extension panels not rendered; no mobile master/detail.
