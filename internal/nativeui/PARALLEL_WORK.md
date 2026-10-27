# Parallel work rules (P1-P4, temporary file, removed in P5)

Several agents work at once in this jj workspace (/www/MCP/Pando/pando-native-desktop).

- Read internal/nativeui/README.md and .kb/pando/features/native_desktop_p0_foundation.md first.
- Go: `GOTOOLCHAIN=go1.27.3 ~/.g/go/bin/go`. Never touch /www/MCP/Pando/pando. No commits, pushes, `git stash`, `jj` mutating commands (jj snapshots automatically). Don't revert files you didn't write.
- Ownership: each agent owns its area packages (internal/nativeui/app/<area>/...). Outside them only ADD files:
  - client endpoints: new file `internal/nativeui/client/<area>.go` (+ `_test.go`); do not rewrite existing client files (small additive edits to types are OK if unavoidable; keep them minimal).
  - new kit components: new file `internal/nativeui/kit/<name>.go`; don't restyle existing kit components (additive options are OK).
  - register the area with a blank import line in internal/nativeui/nativeui.go (one line, keep alphabetical).
  - go.mod: avoid new deps; if one is truly needed, add it and report it.
- Build of the whole tree may be broken briefly by another agent's in-progress edit; re-run, and only fix your own code. Before finishing make sure `go build ./...`, `go vet ./internal/nativeui/...` and your package tests pass.
- Behaviour reference: the React WebUI in web-ui/ (components + packages/pando-client stores) and the server handlers in internal/api. Reach parity with the web feature, using native widgets (MyGo: /tmp/claude-1000/-www-MCP-Pando-pando/b4212b94-d593-4079-9c4b-f355a1d317c2/scratchpad/mygo docs/ui/*.md and examples). Reuse web i18n keys via a.T.
- Testing: headless ui.Tester + httptest; real-app checks with PANDO_NATIVE_PATH / PANDO_NATIVE_CAPTURE (see README) and an isolated HOME. Put screenshots in /tmp/claude-1000/-www-MCP-Pando-pando/b4212b94-d593-4079-9c4b-f355a1d317c2/scratchpad/screens/<area>-*.png. Several agents may run pando instances: use your own isolated HOME and random ports.
- Stop and report only for blockers needing a drastic change in Pando core. Server-side gaps that are small (a missing field/endpoint) may be added in internal/api with tests; report them.
- Finish with a KB summary at .kb/pando/features/native_desktop_<area>.md and a final report: files, verification commands + results, screenshots, gaps.
