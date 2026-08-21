/**
 * Host detection.
 *
 * Only the detection lives here. Reading the desktop configuration needs the
 * Wails-generated bindings, which are produced into this repository by
 * `wails build` and are not part of this package — that half stays in the
 * frontend (see src/services/desktop.ts in the core WebUI).
 */

export interface PandoDesktopConfig {
  apiBase: string
  token: string
}

/** True when running inside the Wails desktop WebView. */
export const isDesktop: boolean =
  typeof window !== 'undefined' && 'go' in (window as unknown as Record<string, unknown>)
