import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { usePWAInstall } from '@/hooks/usePWAInstall'
import { Button, IconButton } from '@/components/ui'
import { X } from '@/components/ui/icons'

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
    <div role="dialog" aria-label={t('pwa.installTitle')} className="pwa-prompt">
      <img src="/pwa-icon-192.png" alt="Pando" className="h-12 w-12 shrink-0 rounded-sm" />

      <div className="min-w-0 flex-1">
        <p className="mb-1 text-sm font-semibold text-fg">{t('pwa.installTitle')}</p>
        <p className="mb-3 text-xs leading-relaxed text-muted">{t('pwa.installDescription')}</p>

        <div className="flex gap-2">
          <Button variant="primary" size="sm" block loading={isInstalling} onClick={() => void handleInstall()}>
            {isInstalling ? t('pwa.installing') : t('pwa.installButton')}
          </Button>
          <Button variant="secondary" size="sm" onClick={handleDismiss}>
            {t('pwa.dismissButton')}
          </Button>
        </div>
      </div>

      <IconButton
        aria-label={t('pwa.dismissButton')}
        icon={<X size={15} />}
        size="sm"
        onClick={handleDismiss}
        className="shrink-0"
      />
    </div>
  )
}
