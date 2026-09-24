import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { computePosition, type Placement } from './position'

export interface TooltipProps {
  content: ReactNode
  children: ReactNode
  placement?: Placement
  /** Hover delay in ms. */
  delay?: number
  disabled?: boolean
}

/**
 * Hover/focus tooltip. Wraps children in an inline-flex span that owns the
 * listeners, so any element (including disabled buttons) can have one.
 */
export function Tooltip({ content, children, placement = 'bottom', delay = 450, disabled }: TooltipProps) {
  const id = useId()
  const anchorRef = useRef<HTMLSpanElement>(null)
  const tipRef = useRef<HTMLDivElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)

  const show = useCallback(() => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setOpen(true), delay)
  }, [delay])
  const hide = useCallback(() => {
    window.clearTimeout(timer.current)
    setOpen(false)
    setPos(null)
  }, [])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  useLayoutEffect(() => {
    if (!open || !anchorRef.current || !tipRef.current) return
    const a = anchorRef.current.getBoundingClientRect()
    const t = tipRef.current.getBoundingClientRect()
    const p = computePosition(a, { width: t.width, height: t.height }, placement)
    setPos({ top: p.top, left: p.left })
  }, [open, placement, content])

  if (disabled || content == null || content === '') return <>{children}</>

  return (
    <span
      ref={anchorRef}
      className="ui-tooltip-anchor"
      onMouseEnter={show}
      onMouseLeave={hide}
      onFocus={show}
      onBlur={hide}
      onPointerDown={hide}
      aria-describedby={open ? id : undefined}
    >
      {children}
      {open &&
        createPortal(
          <div
            ref={tipRef}
            id={id}
            role="tooltip"
            className="ui-tooltip"
            style={pos ? { top: pos.top, left: pos.left } : { top: -9999, left: -9999 }}
          >
            {content}
          </div>,
          document.body,
        )}
    </span>
  )
}
