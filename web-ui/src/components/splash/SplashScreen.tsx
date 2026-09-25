import { useEffect, useState } from 'react'
import clsx from 'clsx'
import { BrandMark } from '@/components/brand'
import '@/styles/splash.css'

export type SplashStatus = 'connecting' | 'authenticating' | 'ready' | 'error'

interface SplashScreenProps {
  status: SplashStatus
  onDone?: () => void
}

const STATUS_TEXT: Record<SplashStatus, string> = {
  connecting: 'Connecting...',
  authenticating: 'Authenticating...',
  ready: 'Ready',
  error: 'Connection failed',
}

const PROGRESS_WIDTH: Record<SplashStatus, string> = {
  connecting: '30%',
  authenticating: '65%',
  ready: '100%',
  error: '100%',
}

export default function SplashScreen({ status, onDone }: SplashScreenProps) {
  const [fadeOut, setFadeOut] = useState(false)

  useEffect(() => {
    if (status === 'ready') {
      const t = setTimeout(() => {
        setFadeOut(true)
        const t2 = setTimeout(() => {
          onDone?.()
        }, 400)
        return () => clearTimeout(t2)
      }, 600)
      return () => clearTimeout(t)
    }
  }, [status, onDone])

  return (
    <div className={clsx('splash-shell', fadeOut && 'splash-shell--fade-out')}>
      {/* Same brand mark as the title bar and the empty chat state, in its app-icon (tile) form. */}
      <div className={clsx('splash-mark', status === 'ready' && 'splash-mark--done')}>
        <BrandMark tile size={64} title="Pando" />
      </div>

      <div className="splash-wordmark brand-display">Pando</div>
      <div className="splash-tagline">AI assistant for code that grows with you</div>

      <div className={clsx('splash-status', status === 'error' && 'splash-status--error')}>{STATUS_TEXT[status]}</div>

      {status !== 'error' && (
        <div className="splash-progress-track">
          <div className="splash-progress-fill" style={{ width: PROGRESS_WIDTH[status] }} />
        </div>
      )}
    </div>
  )
}
