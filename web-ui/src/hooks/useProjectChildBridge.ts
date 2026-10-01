import { useEffect, useMemo, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { isProjectChildMode } from '@pando/client/services/api'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import { createProjectChildBridge } from '@/lib/projectFrameBridge'
import { syncThemeFromParent } from './useTheme'
import { useAgentBusy } from './useAgentBusy'
import { resolveHeaderSection } from '@/components/layout/headerSections'
import { setUIScale } from '@/components/settings/uiScale'

const CHAT_INPUT_SELECTOR = '[data-project-child-focus-target="chat-input"]'

function focusProjectChildInput() {
  const preferred = document.querySelector<HTMLElement>(CHAT_INPUT_SELECTOR)
  if (preferred) {
    preferred.focus()
    return
  }

  const fallback = document.querySelector<HTMLElement>(
    'textarea:not([disabled]), input:not([disabled]), [contenteditable="true"]',
  )
  fallback?.focus()
}

export function useProjectChildBridge() {
  const { i18n, t } = useTranslation()
  const location = useLocation()
  const bridgeRef = useRef<ReturnType<typeof createProjectChildBridge> | null>(null)
  const childMode = isProjectChildMode() && window.parent !== window
  const busy = useAgentBusy()
  const activeSession = useSessionStore((state) =>
    state.sessions.find((session) => session.id === state.activeSessionId) ?? null,
  )
  const extensionPanels = useExtensionPanelsStore((state) => state.panels)
  const section = resolveHeaderSection(location.pathname, (key) => t(key), extensionPanels)

  const title = useMemo(() => {
    const segments = location.pathname.split('/').filter(Boolean)
    const first = segments[0] ?? ''
    if ((first === '' || first === 'chat') && activeSession) {
      return activeSession.title || t('nav.untitledSession')
    }
    return section
  }, [activeSession, location.pathname, section, t])

  useEffect(() => {
    if (!childMode) return

    const bridge = createProjectChildBridge({
      onFocus: focusProjectChildInput,
      onTheme: ({ themeId, accent, uiSize }) => {
        syncThemeFromParent(themeId, accent)
        setUIScale(uiSize, { persist: false })
      },
      onLanguage: ({ lang }) => {
        void i18n.changeLanguage(lang)
        useSettingsStore.setState((state) => ({
          config: { ...state.config, language: lang },
          original: { ...state.original, language: lang },
        }))
      },
    })

    bridgeRef.current = bridge

    const unsubscribeToasts = useToastStore.subscribe((state, previous) => {
      const seen = new Set(previous.toasts.map((toast) => toast.id))
      for (const toast of state.toasts) {
        if (seen.has(toast.id)) continue
        const message = toast.i18nKey ? i18n.t(toast.i18nKey, toast.i18nValues) : toast.message
        if (message) {
          bridge.postNotification(toast.type, message)
        }
      }
    })

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') return

      const isProjectTabShortcut = event.ctrlKey
        && event.altKey
        && !event.metaKey
        && (/^[1-9]$/.test(event.key) || event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key.toLowerCase() === 'w')
      const isShellShortcut = event.ctrlKey
        && !event.altKey
        && !event.metaKey
        && ['p', 'o', 'b'].includes(event.key.toLowerCase())
      const isAutoApproveShortcut = event.shiftKey
        && !event.ctrlKey
        && !event.altKey
        && !event.metaKey
        && event.key === 'Tab'

      if (!(isProjectTabShortcut || isShellShortcut || isAutoApproveShortcut)) {
        return
      }

      event.preventDefault()
      bridge.postShortcut({
        key: event.key,
        ctrl: event.ctrlKey,
        alt: event.altKey,
        shift: event.shiftKey,
        meta: event.metaKey,
      })
    }

    window.addEventListener('keydown', onKeyDown, true)

    return () => {
      window.removeEventListener('keydown', onKeyDown, true)
      unsubscribeToasts()
      bridge.dispose()
      bridgeRef.current = null
    }
  }, [childMode, i18n])

  useEffect(() => {
    if (!childMode) return
    bridgeRef.current?.postTitle(title)
  }, [childMode, title])

  useEffect(() => {
    if (!childMode) return
    bridgeRef.current?.postBusy(busy)
  }, [busy, childMode])
}
