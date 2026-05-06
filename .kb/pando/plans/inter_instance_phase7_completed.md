# Inter-Instance Communication — Phase 7 Completed

**Date:** 2026-05-06  
**Status:** COMPLETED ✓

## What was implemented

### Phase 7: Web-UI Instance Browser

**Goal achieved:** React frontend with instance list, session list, live SSE stream, and remote message/cancel controls. Full Go backend endpoints added.

---

## Backend (Go) — Files Modified

### `internal/api/handlers_instances.go`
4 new handlers added:
- `handleInstanceListSessions` — `GET /api/v1/instances/{id}/sessions` — calls RPC `session.list` on remote instance
- `handleInstanceGetSession` — `GET /api/v1/instances/{id}/sessions/{sid}` — calls RPC `session.get`
- `handleInstanceSessionStream` — `GET /api/v1/instances/{id}/sessions/{sid}/stream` — SSE of ZMQ PUB filtered by `session_id`
- `handleInstanceCancelSession` — `DELETE /api/v1/instances/{id}/sessions/{sid}/cancel` — calls RPC `session.interrupt`

### `internal/api/routes.go`
4 new routes registered (Go 1.22 method prefix pattern):
```
GET    /api/v1/instances/{id}/sessions
GET    /api/v1/instances/{id}/sessions/{sid}
GET    /api/v1/instances/{id}/sessions/{sid}/stream
DELETE /api/v1/instances/{id}/sessions/{sid}/cancel
```

---

## Frontend (React/TypeScript) — Files Created

### `web-ui/src/stores/instancesStore.ts`
Zustand store with:
- State: `instances: InstanceInfo[]`, `remoteSessions: RemoteSession[]`, `selectedInstanceId: string | null`, `loading: boolean`
- Actions: `fetchInstances()`, `selectInstance(id)` (loads remote sessions via GET), `sendRemoteMessage(instanceId, sessionId, content)`, `cancelRemote(instanceId, sessionId)`

### `web-ui/src/components/instances/InstanceCard.tsx`
- Instance card with PRIMARY badge (gold) or SECONDARY badge
- Mode badge colored by type: tui/webui/desktop/acp
- Shows PID and truncated instance ID
- Consistent inline styles with CSS variables

### `web-ui/src/components/instances/RemoteSessionView.tsx`
- Two-column panel: session list (left) + live view (right)
- Session list with title and relative date
- Live stream: connects SSE to `/api/v1/instances/{id}/sessions/{sid}/stream`, renders events in real time
- Inline message input with Send + Cancel buttons
- Calls `sendRemoteMessage` and `cancelRemote` from store

### `web-ui/src/components/instances/InstancesPanel.tsx`
- Main view: instances grouped by `path` (project) on the left + RemoteSessionView on the right
- Refresh button calls `fetchInstances()`
- Auto-fetches on mount

---

## Frontend — Files Modified

### `web-ui/src/App.tsx`
- Added `import InstancesPanel from '@/components/instances/InstancesPanel'`
- Added `<Route path="instances" element={<InstancesPanel />} />` under MainLayout

### `web-ui/src/components/layout/Sidebar.tsx`
- Added `{ path: '/instances', label: t('nav.instances'), icon: faServer }` to NAV_ITEMS
- Imported `faServer` from `@fortawesome/free-solid-svg-icons`

### `web-ui/src/i18n/locales/en.json`
- Added `"nav.instances": "Instances"`

### `web-ui/src/i18n/locales/es.json`
- Added `"nav.instances": "Instancias"`

---

## Full API surface after Phase 7

```
GET    /api/v1/instances                              → list all live instances
GET    /api/v1/instances/{id}                         → get instance detail
GET    /api/v1/instances/{id}/stream                  → SSE proxy of all PUB events
GET    /api/v1/instances/{id}/sessions                → list sessions via RPC
GET    /api/v1/instances/{id}/sessions/{sid}          → get session via RPC
GET    /api/v1/instances/{id}/sessions/{sid}/stream   → SSE of session-filtered PUB
POST   /api/v1/instances/{id}/sessions/{sid}/message  → send message via RPC
DELETE /api/v1/instances/{id}/sessions/{sid}/cancel   → interrupt via RPC
```

---

## Build status

- `go build ./...` — clean
- `npm run build` — clean, 990 modules, no TypeScript errors
