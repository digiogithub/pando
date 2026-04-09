import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { usePWAInstall } from '@/hooks/usePWAInstall'

const DISMISSED_KEY = 'pwa-install-dismissed'

export default function PWAInstallPrompt() {
  const { t } = useTranslation()
  const { canInstall, isInstalled, isInstalling, install } = usePWAInstall()
  const [dismissed, setDismissed] = useState(() => {
    return localStorage.getItem(DISMISSED_KEY) === 'true'
  })
  const [visible, setVisible] = useState(false)

  // Delay appearance so it doesn't compete with the splash screen
  useEffect(() => {
    if (!canInstall || isInstalled || dismissed) return
    const t = setTimeout(() => setVisible(true), 1500)
    return () => clearTimeout(t)
  }, [canInstall, isInstalled, dismissed])

  const handleDismiss = () => {
    setVisible(false)
    localStorage.setItem(DISMISSED_KEY, 'true')
    setDismissed(true)
  }

  const handleInstall = async () => {
    await install()
    setVisible(false)
  }

  if (!visible) return null

  return (
    <div
      role="dialog"
      aria-label={t('pwa.installTitle')}
      style={{
        position: 'fixed',
        bottom: '24px',
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 9000,
        width: 'min(420px, calc(100vw - 32px))',
        background: 'var(--card-bg)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-lg)',
        boxShadow: '0 8px 32px rgba(0,0,0,0.24)',
        padding: 'var(--space-lg)',
        display: 'flex',
        gap: 'var(--space-md)',
        alignItems: 'flex-start',
        animation: 'pwa-slide-up 0.3s ease-out',
      }}
    >
      <style>{`
        @keyframes pwa-slide-up {
          from { opacity: 0; transform: translateX(-50%) translateY(16px); }
          to   { opacity: 1; transform: translateX(-50%) translateY(0); }
        }
      `}</style>

      {/* Icon */}
      <img
        src="/pwa-icon-192.png"
        alt="Pando"
        style={{ width: 48, height: 48, borderRadius: 'var(--radius-sm)', flexShrink: 0 }}
      />

      {/* Text + actions */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <p style={{
          margin: '0 0 4px',
          fontWeight: 600,
          fontSize: '0.95rem',
          color: 'var(--fg)',
        }}>
          {t('pwa.installTitle')}
        </p>
        <p style={{
          margin: '0 0 var(--space-md)',
          fontSize: '0.82rem',
          color: 'var(--fg-muted)',
          lineHeight: 1.4,
        }}>
          {t('pwa.installDescription')}
        </p>

        <div style={{ display: 'flex', gap: 'var(--space-sm)' }}>
          <button
            onClick={handleInstall}
            disabled={isInstalling}
            style={{
              flex: 1,
              padding: '8px 16px',
              background: 'var(--primary)',
              color: 'var(--primary-fg)',
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              cursor: isInstalling ? 'wait' : 'pointer',
              fontWeight: 600,
              fontSize: '0.85rem',
              opacity: isInstalling ? 0.7 : 1,
              transition: 'opacity 0.15s',
            }}
          >
            {isInstalling ? t('pwa.installing') : t('pwa.installButton')}
          </button>
          <button
            onClick={handleDismiss}
            style={{
              padding: '8px 14px',
              background: 'transparent',
              color: 'var(--fg-muted)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              cursor: 'pointer',
              fontSize: '0.85rem',
              transition: 'background 0.15s',
            }}
            onMouseEnter={e => (e.currentTarget.style.background = 'var(--hover)')}
            onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
          >
            {t('pwa.dismissButton')}
          </button>
        </div>
      </div>

      {/* Close × */}
      <button
        onClick={handleDismiss}
        aria-label={t('pwa.dismissButton')}
        style={{
          background: 'transparent',
          border: 'none',
          color: 'var(--fg-dim)',
          cursor: 'pointer',
          fontSize: '1.1rem',
          lineHeight: 1,
          padding: '2px 4px',
          flexShrink: 0,
        }}
      >
        ×
      </button>
    </div>
  )
}
