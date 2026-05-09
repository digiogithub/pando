import { useEffect, useRef, useState, useCallback } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faWifi, faRotateRight } from '@fortawesome/free-solid-svg-icons'
import { useTranslation } from 'react-i18next'
import { useServerStore } from '@/stores/serverStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useSettingsStore } from '@/stores/settingsStore'

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
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.625rem',
        padding: '0.375rem 1rem',
        background: 'var(--error)',
        color: 'white',
        fontSize: 12,
        flexShrink: 0,
        zIndex: 50,
      }}
    >
      <FontAwesomeIcon icon={faWifi} style={{ fontSize: 11, opacity: 0.85 }} />
      <span style={{ flex: 1 }}>{t('common.connectionLost')}</span>
      <button
        onClick={() => void reload()}
        disabled={reloading}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.375rem',
          padding: '0.2rem 0.625rem',
          background: 'rgba(255,255,255,0.18)',
          border: '1px solid rgba(255,255,255,0.35)',
          borderRadius: 'var(--radius-sm)',
          color: 'white',
          fontSize: 11,
          fontWeight: 600,
          cursor: reloading ? 'not-allowed' : 'pointer',
          opacity: reloading ? 0.7 : 1,
          transition: 'background 0.15s',
        }}
        onMouseEnter={(e) => {
          if (!reloading) e.currentTarget.style.background = 'rgba(255,255,255,0.28)'
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.background = 'rgba(255,255,255,0.18)'
        }}
      >
        <FontAwesomeIcon
          icon={faRotateRight}
          style={{ fontSize: 10, animation: reloading ? 'spin 1s linear infinite' : 'none' }}
        />
        {reloading ? t('common.reloading') : t('common.reload')}
      </button>
      <style>{`
        @keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
      `}</style>
    </div>
  )
}
