# Web UI

Pando ships a PWA Web UI written in React and embedded into the binary. Use it locally or
remotely from a browser (it pairs well with a Tailscale or ZeroTier VPN); `pando app` also opens
it inside the native desktop window.

## Features

- **Code editor**: the same editor component as Visual Studio Code, with syntax highlighting and
  LSP features.
- **File explorer**: browse and search project files.
- **Session history**: view and manage past conversations.
- **Real-time updates**: AI responses and tool output stream live over WebSockets.
- **Interactive terminal**: a real shell running in a PTY, streamed to xterm.js over a WebSocket —
  the same experience as the TUI terminal, with full-screen programs (`vim`, `htop`, `less`),
  colors, job control and Ctrl+C. Tabs are independent shells that keep running in the
  background, and a page reload reattaches to them instead of losing them. The older
  single-command endpoint (`POST /api/v1/terminal/exec`), which keeps a dangerous-command filter,
  remains as a fallback for clients that cannot use WebSockets.

> **Security:** like the TUI terminal, the Web UI terminal is an unrestricted shell — anyone who
> can reach the Web UI *and* holds its auth token gets interactive shell access as the user
> running Pando. The server binds to `localhost` by default; think carefully before exposing it
> with `--host 0.0.0.0` (prefer a VPN such as Tailscale/ZeroTier) and enable WebUI Access below.

## WebUI Access (protecting a remotely exposed server)

`pando serve` and `pando app` expose the agent over HTTP — including the bash tool, the
terminal and file writes — so a server bound to anything other than localhost must be
protected. **WebUI Access** adds HTTP Basic Auth in front of the API:

```bash
pando app --host 0.0.0.0   # reachable from the network: credentials are required
pando app                  # bound to localhost: credentials are never asked for
```

Manage it from **Settings → Services → WebUI Access** in the Web UI: switch it on and add
one or more username/password pairs. The rules are:

- Only the `/api/` surface is guarded; static assets stay public so the PWA service worker
  can precache them.
- Credentials are only demanded once the server is exposed, i.e. started on a non-loopback
  host. Bound to localhost the setting stays inert and local development is unaffected.
- Where the request comes from makes no difference: on a `0.0.0.0` bind the browser on your
  own machine is asked to sign in too, because the port is open to the network either way.
- Passwords are stored **age-encrypted** in your config file (`age1:` prefix), exactly like
  provider API keys, using the key set in `~/.config/pando/keys/`.
- Access control cannot be enabled without at least one user, and deleting the last user
  turns it off, so the server can never demand credentials that do not exist.
- The Web UI shows its own login dialog; CLI clients get a standard challenge:

```bash
curl -u admin:secret http://my-host:9999/api/v1/token
```

```toml
[Server]
Enabled     = true
Host        = '0.0.0.0'
Port        = 9999

[Server.BasicAuth]
Enabled = true

[[Server.BasicAuth.Users]]
Username = 'admin'
Password = 'age1:...'   # written by the panel, never by hand
```
