import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'

/**
 * True while the agent (the active streaming session, or any other session
 * with a live run) is working. Drives the title bar `<BrandMark pulse />`
 * busy indicator — the circuit nodes pulse instead of the old kanji-cycling
 * glyph (本 is now the Remembrances mark, so cycling through it would be
 * confusing).
 */
export function useAgentBusy(): boolean {
  const isStreaming = useSessionStore((s) => s.isStreaming)
  const anyRunning = useSessionStore((s) => s.sessions.some((sess) => sess.is_running))
  const activeTabId = useProjectTabsStore((s) => s.activeTabId)
  const activeProjectBusy = useProjectTabsStore((s) =>
    s.activeTabId === 'main'
      ? false
      : s.tabs.find((tab) => tab.projectId === activeTabId)?.busy ?? false,
  )
  return isStreaming || anyRunning || activeProjectBusy
}
