---
created_at: 2026-07-29T09:34:02.250265302Z
updated_at: 2026-07-29T09:34:02.250265302Z
tags:
    - fix
    - api
    - routes
    - ipc
    - panic
---
# Fix: `pando app` panics at startup — ServeMux pattern conflict

Date: 2026-07-29

## Symptom

Latest build/tag of `pando app` panics immediately and exits, writing
`pando-panic-main-<ts>.log` (two observed: 20260729-102937, 20260729-113121).

Stack (abridged):

```
panic: nil pointer dereference
github.com/digiogithub/pando/internal/ipc.(*Bus).Publish(...) internal/ipc/bus.go:154
github.com/digiogithub/pando/internal/ipc/failover.(*Watcher).Shutdown(...) watcher.go:215
github.com/digiogithub/pando/internal/ipc/runtime.Bootstrap.func2() runtime.go:160
panic(...)
net/http.(*ServeMux).register(...)
github.com/digiogithub/pando/internal/api.(*Server).registerRoutes(...) internal/api/routes.go:72
github.com/digiogithub/pando/internal/api.NewServer(...) internal/api/server.go:109
github.com/digiogithub/pando/cmd.runAppMode(...) cmd/app.go:125
```

## Root cause

Primary panic: `http.ServeMux` pattern conflict in `registerRoutes`.

- `mux.HandleFunc("/api/v1/config/lsp/activation", ...)` — no method, so it
  matches **all** methods on a literal path.
- `mux.HandleFunc("DELETE /api/v1/config/lsp/{language}", ...)` — one method,
  more general path.

Go 1.22+ precedence rules make these conflict: neither is strictly more
specific (`DELETE /api/v1/config/lsp/{language} matches fewer methods than
/api/v1/config/lsp/activation, but has a more general path pattern`), so
`ServeMux.register` panics. Reproduced standalone with a 4-line test.

Secondary panic (masking): the deferred `ipc/runtime.Bootstrap` cleanup ran
`failover.Watcher.Shutdown` → `Bus.Publish` on a `Bus` that was never
`Start()`ed, so `b.pubSock` was nil. `Bus.Shutdown` nil-guards it, `Publish`
did not, so the real panic was replaced by a nil deref.

## Changes

- `internal/api/routes.go`: replaced the catch-all activation route with two
  method-scoped registrations (`GET` and `PUT`, the only methods
  `handleConfigLSPActivation` implements), plus a comment explaining the
  conflict. Behavior unchanged for clients; other methods now get ServeMux's
  405 instead of the handler's own `writeError`.
- `internal/ipc/bus.go`: `Bus.Publish` now returns
  `ipc: publish %q: bus not started` when the receiver or `pubSock` is nil,
  instead of dereferencing nil and masking the original panic.
- `internal/api/routes_test.go` (new): `TestRegisterRoutesNoPatternConflict`
  builds the full mux from a zero-value `Server` and fails on panic — a
  regression guard, since any future conflicting route crashes every command
  that constructs an API server (`pando app`, `pando serve`, ...).

## Verification

- `go build ./...` — OK.
- `go test ./internal/api -run 'TestRegisterRoutesNoPatternConflict|LSP'` — ok.
- `go test ./internal/ipc` — ok.
- Conflict confirmed reproducible in isolation before the fix.

Related: [[feature_lsp_ondemand_install_activation]], [[pando_repo_pitfalls]]
