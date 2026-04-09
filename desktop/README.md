# Pando Desktop App

Built with [Wails v2](https://wails.io) — Go backend + React web-ui in a native desktop window.

## Prerequisites

- Go 1.21+
- Node.js 18+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## Development

```bash
make desktop-dev
```

Starts the app with hot reload. The frontend Vite dev server and Go backend reload on changes.

## Build

```bash
make desktop-build
```

Output: `desktop/build/bin/pando-desktop` (Linux/macOS) or `desktop/build/bin/pando-desktop.exe` (Windows)

## How it works

1. On startup, the desktop app starts the Pando HTTP API server on a random port at `127.0.0.1`
2. The React frontend (served via Wails WebView) receives the server URL and auth token via the `GetServerInfo()` Go binding
3. All API calls are routed to the embedded server — no external network access needed
