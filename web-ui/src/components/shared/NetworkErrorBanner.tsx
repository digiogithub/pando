import { useEffect, useRef, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Wifi, RotateCw } from '@/components/ui/icons'
import { Spinner } from '@/components/ui'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'

export default function NetworkErrorBanner() {
  const { t } = useTranslation()
  const connected = useServerStore((s) => s.connected)
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)
  const setActiveSession = useSessionStore((s) => s.setActiveSession)
  const [reloading, setReloading] = useState(false)
  const prevConnected = useRef<boolean>(true)

  const reload = useCallback(async () => {
    setReloading(true)
    try {
      await fetchSessions()
      await fetchSettings()
      const activeId = useSessionStore.getState().activeSessionId
      if (activeId) await setActiveSession(activeId)
    } finally {
      setReloading(false)
    }
  }, [fetchSessions, fetchSettings, setActiveSession])

  // Auto-reload when reconnected after a disconnection
  useEffect(() => {
    if (!prevConnected.current && connected) {
      void reload()
    }
    prevConnected.current = connected
  }, [connected, reload])

  if (connected) return null

  return (
    <div className="banner banner--danger">
      <Wifi size={13} className="opacity-85" />
      <span className="banner-message">{t('common.connectionLost')}</span>
      <button onClick={() => void reload()} disabled={reloading} className="banner-action">
        {reloading ? <Spinner size={11} /> : <RotateCw size={11} />}
        {reloading ? t('common.reloading') : t('common.reload')}
      </button>
    </div>
  )
}
