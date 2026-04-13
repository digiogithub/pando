import { useEffect, useState } from 'react'

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

  const isDark = document.documentElement.dataset.theme === 'dark'

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 9999,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '1.5rem',
        background: isDark
          ? 'var(--bg)'
          : 'var(--bg)', // Using the new clean paper background from tokens
        opacity: fadeOut ? 0 : 1,
        transform: fadeOut ? 'scale(1.01)' : 'scale(1)',
        transition: 'opacity 0.6s ease, transform 0.6s ease',
      }}
    >
      <style>{`
        @keyframes splash-pulse {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.8; transform: scale(0.99); }
        }
        @keyframes splash-fadein {
          from { opacity: 0; transform: translateY(8px); }
          to   { opacity: 1; transform: translateY(0); }
        }
      `}</style>

      {/* Logo oficial — Oriental Symbol 木 */}
      <div
        style={{
          width: 140,
          height: 140,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          animation: status !== 'ready' ? 'splash-pulse 2.5s ease-in-out infinite' : 'none',
        }}
      >
        <span
          style={{
            fontSize: 120,
            lineHeight: 1,
            color: 'var(--primary)',
            fontFamily: 'serif',
            userSelect: 'none',
          }}
        >
          木
        </span>
      </div>

      {/* Title */}
      <div
        style={{
          fontSize: '3.5rem',
          fontWeight: 800,
          letterSpacing: '0.4em',
          color: 'var(--primary)',
          fontFamily: 'serif',
          animation: 'splash-fadein 0.7s ease',
          userSelect: 'none',
        }}
      >
        PANDO
      </div>

      {/* Subtitle */}
      <div
        style={{
          fontSize: 13,
          color: 'var(--fg-muted)',
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          animation: 'splash-fadein 0.7s ease 0.15s both',
        }}
      >
        AI assistant for code that grows with you
      </div>

      {/* Status */}
      <div
        style={{
          marginTop: '1.5rem',
          fontSize: 12,
          color: status === 'error' ? 'var(--error)' : 'var(--fg-dim)',
          fontFamily: 'monospace',
          animation: 'splash-fadein 0.7s ease 0.3s both',
          minWidth: 160,
          textAlign: 'center',
        }}
      >
        {STATUS_TEXT[status]}
      </div>

      {/* Progress bar for non-error states — sharp edges */}
      {status !== 'error' && (
        <div
          style={{
            width: 240,
            height: 2,
            background: 'var(--border)',
            borderRadius: 0,
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              height: '100%',
              background: 'var(--primary)',
              borderRadius: 0,
              width:
                status === 'connecting'
                  ? '30%'
                  : status === 'authenticating'
                  ? '65%'
                  : '100%',
              transition: 'width 0.8s cubic-bezier(0.4, 0, 0.2, 1)',
            }}
          />
        </div>
      )}

    </div>
  )
}
