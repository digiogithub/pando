import { useEffect, useId, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import clsx from 'clsx'
import { IconButton } from './IconButton'
import { X } from './icons'
import { FOCUSABLE } from './position'

export interface DialogProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  description?: ReactNode
  /** Footer actions, right-aligned (primary action last). */
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl'
  /** Close when clicking the overlay (default true). */
  dismissible?: boolean
  /** Hide the header close button. */
  hideClose?: boolean
  /** Accessible label for the close button (pass a translated string). */
  closeLabel?: string
  className?: string
  children?: ReactNode
}

/**
 * Modal dialog: portal to <body>, blurred scrim, focus trap, Esc to close,
 * focus restored to the previously focused element on close.
 */
export function Dialog({
  open,
  onClose,
  title,
  description,
  footer,
  size = 'md',
  dismissible = true,
  hideClose,
  closeLabel = 'Close',
  className,
  children,
}: DialogProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const descId = useId()
  const onCloseRef = useRef(onClose)

  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  useEffect(() => {
    if (!open) return
    const previous = document.activeElement as HTMLElement | null
    const panel = panelRef.current
    const first = panel?.querySelector<HTMLElement>('[autofocus], [data-autofocus]') ?? panel?.querySelector<HTMLElement>(FOCUSABLE)
    ;(first ?? panel)?.focus()

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // Consumed here: page-level Esc handlers check defaultPrevented.
        e.preventDefault()
        e.stopPropagation()
        onCloseRef.current()
        return
      }
      if (e.key !== 'Tab' || !panel) return
      const items = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.offsetParent !== null)
      if (items.length === 0) {
        e.preventDefault()
        panel.focus()
        return
      }
      const firstEl = items[0]
      const lastEl = items[items.length - 1]
      if (e.shiftKey && (document.activeElement === firstEl || document.activeElement === panel)) {
        e.preventDefault()
        lastEl.focus()
      } else if (!e.shiftKey && document.activeElement === lastEl) {
        e.preventDefault()
        firstEl.focus()
      }
    }
    document.addEventListener('keydown', onKey, true)
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey, true)
      document.body.style.overflow = prevOverflow
      previous?.focus?.()
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div
      className="ui-dialog-overlay"
      onMouseDown={(e) => {
        if (dismissible && e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        aria-describedby={description ? descId : undefined}
        tabIndex={-1}
        className={clsx('ui-dialog', `ui-dialog--${size}`, className)}
      >
        {(title || !hideClose) && (
          <div className="ui-dialog-header">
            <div className="ui-dialog-titles">
              {title && <h2 id={titleId} className="ui-dialog-title">{title}</h2>}
              {description && <p id={descId} className="ui-dialog-description">{description}</p>}
            </div>
            {!hideClose && <IconButton aria-label={closeLabel} icon={<X />} size="sm" onClick={onClose} />}
          </div>
        )}
        {children != null && <div className="ui-dialog-body">{children}</div>}
        {footer && <div className="ui-dialog-footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}
