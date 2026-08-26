/**
 * Direct access to the Wails-injected bindings.
 *
 * wailsBindings.ts dynamically imports `wailsjs/go/desktop/App`, a file Wails
 * generates at build time. It is not present in this workspace, so every call
 * there falls back to the web behaviour. Wails also injects the same methods on
 * `window.go.desktop.App` at runtime, with no generated file involved — which
 * is what the Design surface uses, because "open in the real browser" and "save
 * this export" have no working web equivalent inside a webview.
 *
 * Each helper degrades to the browser behaviour when the binding is absent, so
 * the same component works in a tab and in the desktop shell.
 */

interface DesktopBindings {
  OpenInBrowser?: (url: string) => Promise<void> | void
  SaveDownload?: (url: string, defaultFilename: string) => Promise<string>
}

function bindings(): DesktopBindings | null {
  const go = (window as unknown as { go?: { desktop?: { App?: DesktopBindings } } }).go
  return go?.desktop?.App ?? null
}

export function hasDesktopAppBindings(): boolean {
  return bindings() !== null
}

/** Opens a URL outside the webview; a normal new tab in a browser. */
export async function openExternal(url: string): Promise<void> {
  const app = bindings()
  if (app?.OpenInBrowser) {
    await app.OpenInBrowser(url)
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

/**
 * Saves a URL to disk. In the desktop shell the fetch and the write happen on
 * the Go side behind a native save dialog; a webview download would otherwise
 * silently go nowhere. Returns the written path, or "" in a browser and on
 * cancel.
 */
export async function saveUrlToDisk(url: string, defaultFilename: string): Promise<string> {
  const app = bindings()
  if (app?.SaveDownload) {
    return app.SaveDownload(url, defaultFilename)
  }
  window.open(url, '_blank', 'noopener')
  return ''
}
