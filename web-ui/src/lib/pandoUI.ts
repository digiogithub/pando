import * as React from 'react'
import api, { getBaseURL } from '@pando/client/services/api'

/**
 * The `window.__PANDO_UI__` host contract.
 *
 * An extension panel is a built ES module served by the backend and imported
 * at run time. It is compiled separately from this shell, possibly against a
 * different core version, so the surface it may rely on has to be small,
 * explicit and versioned — that is what this file is.
 *
 * A panel's default export is a mount function, not a React component:
 *
 *   export default function mount(el, ctx) {
 *     el.textContent = 'hello'
 *     return () => { ... }   // optional cleanup
 *   }
 *
 * Handing over a DOM node rather than rendering a component is deliberate. It
 * keeps a panel free of any React-version agreement with the shell: a panel
 * that wants React uses `ctx.react`, which is *this* shell's instance, so
 * hooks and context work and React is never loaded twice.
 */

/** Bumped only on a breaking change. A panel may refuse to mount on a version it does not know. */
export const PANDO_UI_VERSION = 1

export interface PandoUIContext {
  /** Contract version. */
  version: number
  /** The shell's React instance. Panels must not bundle their own. */
  react: typeof React
  /** The Pando REST client, already carrying the auth token. */
  api: typeof api
  /** Base URL the API is served from; '' means same origin. */
  apiBase: string
  /** The panel being mounted, namespaced with its extension ID. */
  panelId: string
  /** The extension that contributed the panel. */
  extensionId: string
}

/**
 * A panel module: a default-exported mount function returning an optional
 * cleanup, called when the panel unmounts.
 */
export type PanelModule = {
  default: (el: HTMLElement, ctx: PandoUIContext) => void | (() => void) | Promise<void | (() => void)>
}

/**
 * Installs the host contract on window.
 *
 * Panels are imported as plain modules, so they cannot be handed the context
 * as an argument at import time; a global is how they reach the shell before
 * their mount function is called. It is installed once, at boot, before any
 * panel is imported.
 */
export function installPandoUI(): void {
  const host = {
    version: PANDO_UI_VERSION,
    react: React,
    api,
    get apiBase() {
      return getBaseURL()
    },
  }
  ;(window as unknown as Record<string, unknown>).__PANDO_UI__ = host
}

/** Builds the per-panel context handed to a mount function. */
export function panelContext(panelId: string, extensionId: string): PandoUIContext {
  return {
    version: PANDO_UI_VERSION,
    react: React,
    api,
    apiBase: getBaseURL(),
    panelId,
    extensionId,
  }
}
