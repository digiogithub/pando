# `@pando/client`

The transport and state layer of the Pando WebUI: REST client, SSE
subscriptions, Zustand stores, the hooks built directly on them, and the
TypeScript types shared by all of it.

It exists because Pando has more than one frontend. The enterprise build ships
a different visual layer under a different brand, but its communications with
the Pando API are **identical** to core's. Keeping that layer here means the
two frontends cannot drift: a change to an endpoint, an SSE event or a payload
shape is made once, and a mismatch is a TypeScript error in both frontends
rather than a runtime surprise in one of them.

## The rule

This package contains **no presentation**. No JSX, no CSS, no i18n, no icons,
no component imports — those belong to the frontend consuming it. A hook lives
here when it is state over the API (`useChat`, `useGoal`); it stays in the
frontend when it is about how things look or about the host platform
(`useTheme`, `usePWAInstall`).

The dependency direction is one-way and enforced by that rule: frontends import
this package, this package imports nothing from any frontend.

## Layout

| Path | What |
|---|---|
| `src/services/` | REST client (`api`), SSE streams, auth, mappers, PTY, desktop/Wails bridges |
| `src/stores/` | Zustand stores, one per domain |
| `src/hooks/` | React hooks that are pure API/state (`useChat`, `useGoal`) |
| `src/types/` | The shared TypeScript types |

## Importing

Subpath imports keep the module graph explicit and the default exports intact:

```ts
import api from '@pando/client/services/api'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { Session } from '@pando/client/types'
```

The root barrel (`@pando/client`) re-exports the same surface for consumers
that prefer one entry point.

## Versioning

The package is versioned independently of the app. Treat its exported surface
as a contract: a breaking change here breaks a frontend that is not in this
repository.
