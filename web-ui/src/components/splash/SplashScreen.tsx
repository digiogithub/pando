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
        gap: '1rem',
        background: isDark
          ? '#1e1e2e'
          : 'radial-gradient(ellipse at center, #FFFDF8 0%, #F6F0E3 50%, #EEE5D2 100%)',
        opacity: fadeOut ? 0 : 1,
        transform: fadeOut ? 'scale(1.02)' : 'scale(1)',
        transition: 'opacity 0.4s ease, transform 0.4s ease',
      }}
    >
      <style>{`
        @keyframes splash-pulse {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.75; transform: scale(0.97); }
        }
        @keyframes splash-fadein {
          from { opacity: 0; transform: translateY(12px); }
          to   { opacity: 1; transform: translateY(0); }
        }
        @keyframes splash-dots {
          0%   { content: ''; }
          33%  { content: '.'; }
          66%  { content: '..'; }
          100% { content: '...'; }
        }
      `}</style>

      {/* Logo tree */}
      <div
        style={{
          fontSize: 72,
          lineHeight: 1,
          animation: status !== 'ready' ? 'splash-pulse 2s ease-in-out infinite' : 'none',
        }}
      >
        🌳
      </div>

      {/* Title */}
      <div
        style={{
          fontSize: '3rem',
          fontWeight: 800,
          letterSpacing: '0.3em',
          color: 'var(--primary)',
          animation: 'splash-fadein 0.5s ease',
        }}
      >
        PANDO
      </div>

      {/* Subtitle */}
      <div
        style={{
          fontSize: 14,
          color: 'var(--fg-muted)',
          letterSpacing: '0.04em',
          animation: 'splash-fadein 0.5s ease 0.1s both',
        }}
      >
        AI assistant for code that grows with you
      </div>

      {/* Status */}
      <div
        style={{
          marginTop: '1.5rem',
          fontSize: 13,
          color: status === 'error' ? 'var(--error)' : 'var(--fg-dim)',
          animation: 'splash-fadein 0.5s ease 0.2s both',
          minWidth: 160,
          textAlign: 'center',
        }}
      >
        {STATUS_TEXT[status]}
      </div>

      {/* Progress bar for non-error states */}
      {status !== 'error' && (
        <div
          style={{
            width: 200,
            height: 3,
            background: 'var(--border)',
            borderRadius: 2,
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              height: '100%',
              background: 'var(--primary)',
              borderRadius: 2,
              width:
                status === 'connecting'
                  ? '30%'
                  : status === 'authenticating'
                  ? '65%'
                  : '100%',
              transition: 'width 0.5s ease',
            }}
          />
        </div>
      )}
    </div>
  )
}
