import api from './api'

/**
 * UI contributed by compiled-in extension modules.
 *
 * Which panels exist is decided when the binary is built, so this is fetched
 * once at boot and never changes for the life of the process. A standard build
 * returns an empty list.
 */

/** One panel, as the backend resolves it: Entry is already a loadable URL. */
export interface ExtensionPanel {
  id: string
  extension: string
  title: string
  slot: ExtensionSlotName
  entry: string
  icon?: string
  order: number
}

/**
 * The slots the shell reserves. Kept in sync by hand with the Slot* constants
 * in pkg/extension/frontend.go — the backend already drops panels asking for a
 * slot it does not know, so an unknown value never reaches the browser.
 */
export type ExtensionSlotName = 'sidebar' | 'settings' | 'chat-side' | 'status-bar'

interface UIManifestResponse {
  panels: ExtensionPanel[] | null
}

/**
 * Fetches the panel manifest. Errors are the caller's to handle: a failure
 * here must never stop the shell from rendering, since panels are additive.
 */
export async function fetchExtensionPanels(): Promise<ExtensionPanel[]> {
  const res = await api.get<UIManifestResponse>('/api/v1/extensions/ui')
  return res.panels ?? []
}
