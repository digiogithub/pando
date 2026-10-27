package api

import (
	"net/http"
)

// extensionHostPage is the minimal host document for one extension panel
// outside the React WebUI shell (the native desktop shows it in an embedded
// web view). It provides the window.__PANDO_UI__ v1 contract of
// web-ui/src/lib/pandoUI.ts: version, api (get/post/put/patch/delete/getToken
// against this origin, carrying the API token), apiBase, and panelId /
// extensionId on the context handed to the panel's mount function.
//
// The panel id and the token come from the query string (?panel=&token=) and
// are never interpolated into the markup. ctx.react is the one thing a
// standalone host cannot offer (the shell's React instance lives in the
// bundled WebUI): it is a stub that throws a readable error naming the
// limitation, so a panel that needs React degrades into the error box (the
// native shell then offers "open in browser", which loads the WebUI).
const extensionHostPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pando extension</title>
<style>
:root{color-scheme:light dark;--bg:#ffffff;--fg:#1a1a1a;--fg-muted:#666;--border:#d9d9d9;--accent:#2563eb;--danger:#dc2626;--success:#16a34a;--warning:#d97706}
:root[data-theme=dark]{--bg:#16181d;--fg:#e8e8ea;--fg-muted:#9a9aa3;--border:#2c2f36;--accent:#60a5fa;--danger:#f87171;--success:#4ade80;--warning:#fbbf24}
html,body{margin:0;padding:0;background:var(--bg);color:var(--fg);font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif}
#root{padding:16px}
.pando-ext-error{border:1px solid var(--danger);color:var(--danger);border-radius:8px;padding:10px 12px;font-size:13px;white-space:pre-wrap}
</style>
</head>
<body>
<div id="root" data-extension-panel=""></div>
<script type="module">
const params = new URLSearchParams(location.search);
const panelId = params.get('panel') || '';
let token = params.get('token') || sessionStorage.getItem('pando-token') || '';
if (token) {
  sessionStorage.setItem('pando-token', token);
}
const theme = params.get('theme');
if (theme === 'dark' || theme === 'light') {
  document.documentElement.dataset.theme = theme;
} else if (matchMedia('(prefers-color-scheme: dark)').matches) {
  document.documentElement.dataset.theme = 'dark';
}
// Keep the token out of the visible URL (history, bug reports).
if (params.has('token')) {
  params.delete('token');
  const q = params.toString();
  history.replaceState(null, '', location.pathname + (q ? '?' + q : ''));
}

async function fetchApi(path, init = {}) {
  const headers = Object.assign({ 'Content-Type': 'application/json', 'X-Pando-Client': 'desktop' }, init.headers || {});
  if (token) headers['X-Pando-Token'] = token;
  const res = await fetch(path, Object.assign({}, init, { headers }));
  if (res.status === 401) throw new Error('Unauthorized');
  if (!res.ok) throw new Error((await res.text()) || 'HTTP ' + res.status);
  const type = res.headers.get('Content-Type') || '';
  return type.includes('application/json') ? res.json() : res.text();
}
const api = {
  get: (path) => fetchApi(path),
  post: (path, body, options = {}) => fetchApi(path, Object.assign({}, options, { method: 'POST', body: JSON.stringify(body) })),
  patch: (path, body) => fetchApi(path, { method: 'PATCH', body: JSON.stringify(body) }),
  put: (path, body) => fetchApi(path, { method: 'PUT', body: JSON.stringify(body) }),
  delete: (path) => fetchApi(path, { method: 'DELETE' }),
  getToken: () => token || null,
  setToken: (t) => { token = t; },
  removeToken: () => { token = ''; },
};
const noReact = new Proxy({}, {
  get() {
    throw new Error('this panel needs the shell React instance (ctx.react), which the native desktop host page does not provide. Open it in the browser UI instead.');
  },
});
const VERSION = 1;
window.__PANDO_UI__ = { version: VERSION, react: noReact, api, get apiBase() { return ''; } };

const root = document.getElementById('root');
function fail(message) {
  root.replaceChildren();
  const box = document.createElement('div');
  box.className = 'pando-ext-error';
  box.textContent = (panelId || 'panel') + ': ' + message;
  root.appendChild(box);
}

let cleanup;
try {
  if (!panelId) throw new Error('missing ?panel=');
  const manifest = await api.get('/api/v1/extensions/ui');
  const panel = (manifest.panels || []).find((p) => p.id === panelId);
  if (!panel) throw new Error('unknown extension panel');
  root.dataset.extensionPanel = panel.id;
  document.title = panel.title || panel.id;
  const mod = await import(panel.entry);
  if (typeof mod.default !== 'function') throw new Error('panel has no default export');
  const ctx = { version: VERSION, react: noReact, api, apiBase: '', panelId: panel.id, extensionId: panel.extension };
  cleanup = await mod.default(root, ctx);
} catch (err) {
  fail(err instanceof Error ? err.message : String(err));
}
addEventListener('pagehide', () => {
  try { if (typeof cleanup === 'function') cleanup(); } catch (_) { /* ignore */ }
});
</script>
</body>
</html>
`

// handleExtensionsHost serves the host page of an extension panel:
// GET /api/v1/extensions/host?panel=<id>[&token=<api token>][&theme=dark|light].
// The route sits behind the API token check; the embedded web view passes the
// token as ?token= because a navigation cannot carry a header.
func (s *Server) handleExtensionsHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(extensionHostPage))
}
