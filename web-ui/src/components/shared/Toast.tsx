import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { CircleCheck, CircleX, TriangleAlert, Info, X } from '@/components/ui/icons'
import { IconButton } from '@/components/ui'
import { useToastStore, type Toast } from '@pando/client/stores/toastStore'

const TOAST_ICONS = {
  success: CircleCheck,
  error: CircleX,
  warning: TriangleAlert,
  info: Info,
}

function ToastItem({ toast }: { toast: Toast }) {
  const { t } = useTranslation()
  const removeToast = useToastStore((s) => s.removeToast)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    requestAnimationFrame(() => el.classList.add('toast--visible'))
  }, [])

  const Icon = TOAST_ICONS[toast.type]
  const message = toast.i18nKey ? t(toast.i18nKey, toast.i18nValues) : toast.message

  return (
    <div ref={ref} className="toast">
      <Icon size={16} className={`toast-icon toast-icon--${toast.type}`} />
      <span className="toast-message">{message}</span>
      <IconButton
        aria-label="Close notification"
        icon={<X size={13} />}
        size="sm"
        onClick={() => removeToast(toast.id)}
      />
    </div>
  )
}

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts)

  if (toasts.length === 0) return null

  return (
    <div className="toast-stack">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} />
      ))}
    </div>
  )
}

export default ToastContainer
