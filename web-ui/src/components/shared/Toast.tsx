import { useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faCheckCircle,
  faTimesCircle,
  faExclamationTriangle,
  faInfoCircle,
  faTimes,
} from '@fortawesome/free-solid-svg-icons'
import { useToastStore, type Toast, type ToastType } from '@/stores/toastStore'

const TOAST_COLORS: Record<ToastType, string> = {
  success: 'var(--success)',
  error: 'var(--error)',
  warning: 'var(--warning)',
  info: 'var(--info)',
}

const TOAST_ICONS = {
  success: faCheckCircle,
  error: faTimesCircle,
  warning: faExclamationTriangle,
  info: faInfoCircle,
}

function ToastItem({ toast }: { toast: Toast }) {
  const removeToast = useToastStore((s) => s.removeToast)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    requestAnimationFrame(() => {
      el.style.opacity = '1'
      el.style.transform = 'translateX(0)'
    })
  }, [])

  const color = TOAST_COLORS[toast.type]
  const icon = TOAST_ICONS[toast.type]

  return (
    <div
      ref={ref}
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: '0.75rem',
        padding: '0.75rem 1rem',
        background: 'var(--card-bg)',
        border: '1px solid var(--border)',
        borderLeft: `4px solid ${color}`,
        borderRadius: 'var(--radius-md)',
        boxShadow: '0 4px 16px rgba(0,0,0,0.12)',
        maxWidth: 360,
        minWidth: 240,
        opacity: 0,
        transform: 'translateX(24px)',
        transition: 'opacity 0.25s ease, transform 0.25s ease',
        position: 'relative',
      }}
    >
      <FontAwesomeIcon
        icon={icon}
        style={{ color, fontSize: 15, marginTop: 2, flexShrink: 0 }}
      />
      <span
        style={{
          flex: 1,
          fontSize: 13,
          color: 'var(--fg)',
          lineHeight: '1.4',
          wordBreak: 'break-word',
        }}
      >
        {toast.message}
      </span>
      <button
        onClick={() => removeToast(toast.id)}
        style={{
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--fg-dim)',
          padding: '0 0 0 4px',
          fontSize: 12,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
        }}
        aria-label="Close notification"
      >
        <FontAwesomeIcon icon={faTimes} />
      </button>
    </div>
  )
}

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts)

  if (toasts.length === 0) return null

  return (
    <div
      style={{
        position: 'fixed',
        bottom: '1.5rem',
        right: '1.5rem',
        zIndex: 2000,
        display: 'flex',
        flexDirection: 'column',
        gap: '0.5rem',
        pointerEvents: 'none',
      }}
    >
      {toasts.map((toast) => (
        <div key={toast.id} style={{ pointerEvents: 'auto' }}>
          <ToastItem toast={toast} />
        </div>
      ))}
    </div>
  )
}

export default ToastContainer
