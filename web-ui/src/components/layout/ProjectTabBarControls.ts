import { useCallback, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { sessionBrowserStorage } from '@pando/client/services/storage'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore, type ProjectTabActionResult } from '@pando/client/stores/projectTabsStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import { useDialogs } from '@/components/shared/useDialogs'

const PROJECT_TAB_CLOSE_PREF_KEY = 'pando_project_tab_close_choice'

export const SHELL_MAIN_PANEL_ID = 'shell-main-panel'

type CloseChoice = 'stop' | 'keep'
type CloseMode = CloseChoice | 'prompt'

interface ClosePreference {
  stop: CloseChoice
}

export interface ProjectTabShortcutController {
  focusTabByIndex: (index: number) => void
  closeActiveProjectTab: () => void | Promise<void>
  cycleTabs: (direction: -1 | 1) => void
}

export interface ProjectTabBarController extends ProjectTabShortcutController {
  dialogs: ReactNode
  focusTab: (id: 'main' | string) => void
  closeProjectTab: (projectId: string, mode?: CloseMode) => Promise<void>
  restartProjectTab: (projectId: string) => Promise<void>
  revealProject: () => void
  openProjectInNewWindow: (projectId: string) => Promise<void>
}

export interface ShellShortcutEvent {
  ctrlKey: boolean
  altKey: boolean
  metaKey: boolean
  shiftKey: boolean
  key: string
  code?: string
}

export interface ShellShortcutActions {
  openQuickMenu: () => void
  openModelSwitcher: () => void
  toggleSidebar: () => void
  toggleAutoApprove: () => void
}

function readClosePreference(): ClosePreference | null {
  try {
    const raw = sessionBrowserStorage.getItem(PROJECT_TAB_CLOSE_PREF_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<ClosePreference>
    return parsed.stop === 'stop' || parsed.stop === 'keep'
      ? { stop: parsed.stop }
      : null
  } catch {
    return null
  }
}

function writeClosePreference(choice: CloseChoice) {
  sessionBrowserStorage.setItem(PROJECT_TAB_CLOSE_PREF_KEY, JSON.stringify({ stop: choice } satisfies ClosePreference))
}

function translateNotice(
  t: (key: string, options?: Record<string, unknown>) => string,
  result: ProjectTabActionResult,
) {
  const notice = result.notice
  if (!notice) return

  const message = result.error
    ? `${t(notice.key, notice.values)}: ${result.error}`
    : t(notice.key, notice.values)

  useToastStore.getState().addToast(message, notice.type)
}

export function handleProjectTabKeyboardShortcut(
  event: ShellShortcutEvent,
  controller: ProjectTabShortcutController,
): boolean {
  if (!event.ctrlKey || !event.altKey || event.metaKey) {
    return false
  }

  if ((event.code && /^Digit[1-9]$/.test(event.code)) || /^[1-9]$/.test(event.key)) {
    const key = Number((event.code ? event.code.replace('Digit', '') : '') || event.key)
    if (Number.isInteger(key) && key >= 1 && key <= 9) {
      controller.focusTabByIndex(key)
      return true
    }
  }

  if (event.code === 'KeyW') {
    void controller.closeActiveProjectTab()
    return true
  }

  if (event.key === 'ArrowLeft') {
    controller.cycleTabs(-1)
    return true
  }

  if (event.key === 'ArrowRight') {
    controller.cycleTabs(1)
    return true
  }

  return false
}

export function handleShellKeyboardShortcut(
  event: ShellShortcutEvent,
  controller: ProjectTabShortcutController,
  actions: ShellShortcutActions,
): boolean {
  const key = event.key.toLowerCase()
  if (handleProjectTabKeyboardShortcut(event, controller)) {
    return true
  }

  if (event.ctrlKey && !event.altKey && !event.metaKey && key === 'p') {
    actions.openQuickMenu()
    return true
  }

  if (event.ctrlKey && !event.altKey && !event.metaKey && key === 'o') {
    actions.openModelSwitcher()
    return true
  }

  if (event.ctrlKey && !event.altKey && !event.metaKey && key === 'b') {
    actions.toggleSidebar()
    return true
  }

  if (event.shiftKey && event.key === 'Tab' && !event.ctrlKey && !event.altKey && !event.metaKey) {
    actions.toggleAutoApprove()
    return true
  }

  return false
}

export function useProjectTabBarController(): ProjectTabBarController {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const openProjectDesktop = useProjectStore((state) => state.openProjectDesktop)
  const { confirm, dialogs } = useDialogs()

  const focusTab = useCallback((id: 'main' | string) => {
    useProjectTabsStore.getState().focusTab(id)
  }, [])

  const focusTabByIndex = useCallback((index: number) => {
    const ordered = ['main', ...useProjectTabsStore.getState().tabs.map((tab) => tab.projectId)]
    const next = ordered[index - 1]
    if (next) {
      useProjectTabsStore.getState().focusTab(next)
    }
  }, [])

  const cycleTabs = useCallback((direction: -1 | 1) => {
    const store = useProjectTabsStore.getState()
    const ordered = ['main', ...store.tabs.map((tab) => tab.projectId)]
    if (ordered.length <= 1) return

    const currentIndex = Math.max(ordered.indexOf(store.activeTabId), 0)
    const nextIndex = (currentIndex + direction + ordered.length) % ordered.length
    const next = ordered[nextIndex]
    if (next) {
      store.focusTab(next)
    }
  }, [])

  const requestCloseMode = useCallback(async (): Promise<CloseChoice | 'cancel'> => {
    const remembered = readClosePreference()
    if (remembered) {
      return remembered.stop
    }

    const shouldStop = await confirm({
      title: t('projects.tabs.closeConfirmTitle'),
      message: t('projects.tabs.closeConfirmMessage'),
      confirmLabel: t('projects.tabs.closeAndStop'),
      dangerous: true,
    })
    if (shouldStop) {
      writeClosePreference('stop')
      return 'stop'
    }

    const shouldKeepRunning = await confirm({
      title: t('projects.tabs.keepRunningTitle'),
      message: t('projects.tabs.keepRunningMessage'),
      confirmLabel: t('projects.tabs.close'),
    })
    if (shouldKeepRunning) {
      writeClosePreference('keep')
      return 'keep'
    }

    return 'cancel'
  }, [confirm, t])

  const closeProjectTab = useCallback(
    async (projectId: string, mode: CloseMode = 'prompt') => {
      if (!projectId) return

      let resolvedMode: CloseChoice | 'cancel'
      if (mode === 'prompt') {
        resolvedMode = await requestCloseMode()
      } else {
        resolvedMode = mode
      }
      if (resolvedMode === 'cancel') return

      const result = await useProjectTabsStore.getState().closeTab(
        projectId,
        resolvedMode === 'stop' ? { stop: true } : undefined,
      )
      translateNotice(t, result)
    },
    [requestCloseMode, t],
  )

  const closeActiveProjectTab = useCallback(async () => {
    const active = useProjectTabsStore.getState().activeTabId
    if (active === 'main') return
    await closeProjectTab(active)
  }, [closeProjectTab])

  const restartProjectTab = useCallback(
    async (projectId: string) => {
      const result = await useProjectTabsStore.getState().restartTab(projectId)
      translateNotice(t, result)
    },
    [t],
  )

  const revealProject = useCallback(() => {
    navigate('/projects')
  }, [navigate])

  const openProjectInNewWindow = useCallback(
    async (projectId: string) => {
      await openProjectDesktop(projectId)
    },
    [openProjectDesktop],
  )

  return {
    dialogs,
    focusTab,
    focusTabByIndex,
    cycleTabs,
    closeProjectTab,
    closeActiveProjectTab,
    restartProjectTab,
    revealProject,
    openProjectInNewWindow,
  }
}
