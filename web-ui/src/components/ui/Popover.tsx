import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import clsx from 'clsx'
import { computePosition, type Placement } from './position'

export interface PopoverProps {
  open: boolean
  onClose: () => void
  /** Element the popover is anchored to (usually the trigger button). */
  anchorRef: RefObject<HTMLElement | null>
  placement?: Placement
  offset?: number
  /** Adds inner padding (off for menus). */
  padded?: boolean
  className?: string
  role?: string
  children: ReactNode
  /** Extra props for the floating element (aria-*, id, onKeyDown...). */
  floatingProps?: React.HTMLAttributes<HTMLDivElement>
  floatingRef?: RefObject<HTMLDivElement | null>
}

/**
 * Anchored floating panel: portal, flips/clamps to the viewport, closes on
 * outside click, Esc and window blur; repositions on scroll/resize.
 */
export function Popover({
  open,
  onClose,
  anchorRef,
  placement = 'bottom-start',
  offset = 6,
  padded = true,
  className,
  role = 'dialog',
  children,
  floatingProps,
  floatingRef,
}: PopoverProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const setRef = useCallback(
    (el: HTMLDivElement | null) => {
      ref.current = el
      if (floatingRef) floatingRef.current = el
    },
    [floatingRef],
  )
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const onCloseRef = useRef(onClose)

  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  const update = useCallback(() => {
    const anchor = anchorRef.current
    const el = ref.current
    if (!anchor || !el) return
    const p = computePosition(anchor.getBoundingClientRect(), { width: el.offsetWidth, height: el.offsetHeight }, placement, offset)
    setPos({ top: p.top, left: p.left })
  }, [anchorRef, placement, offset])

  useLayoutEffect(() => {
    if (!open) {
      setPos(null)
      return
    }
    update()
  }, [open, update])

  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      const target = e.target as Node
      if (ref.current?.contains(target) || anchorRef.current?.contains(target)) return
      onCloseRef.current()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // Consumed here: page-level Esc handlers check defaultPrevented.
        e.preventDefault()
        e.stopPropagation()
        onCloseRef.current()
        anchorRef.current?.focus?.()
      }
    }
    const onBlur = () => onCloseRef.current()
    document.addEventListener('pointerdown', onPointer, true)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    window.addEventListener('blur', onBlur)
    return () => {
      document.removeEventListener('pointerdown', onPointer, true)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('blur', onBlur)
    }
  }, [open, update, anchorRef])

  if (!open) return null
  return createPortal(
    <div
      {...floatingProps}
      ref={setRef}
      role={role}
      className={clsx('ui-popover', padded && 'ui-popover--pad', className)}
      style={pos ? { top: pos.top, left: pos.left } : { top: -9999, left: -9999 }}
    >
      {children}
    </div>,
    document.body,
  )
}

// ── Menu ────────────────────────────────────────────────────────────────────

export interface MenuProps extends Omit<PopoverProps, 'padded' | 'role' | 'floatingProps' | 'floatingRef'> {
  'aria-label'?: string
}

const ITEM_SELECTOR = '[role="menuitem"]:not([disabled]), [role="menuitemcheckbox"]:not([disabled]), [role="menuitemradio"]:not([disabled])'

/**
 * Dropdown menu on top of Popover. Arrow keys / Home / End move focus,
 * Enter/Space activate, Esc closes and returns focus to the anchor.
 */
export function Menu({ className, children, open, 'aria-label': ariaLabel, ...rest }: MenuProps) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    // Wait one frame so the portal content exists and is positioned.
    const raf = requestAnimationFrame(() => {
      ref.current?.querySelector<HTMLElement>(ITEM_SELECTOR)?.focus()
    })
    return () => cancelAnimationFrame(raf)
  }, [open])

  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(ref.current?.querySelectorAll<HTMLElement>(ITEM_SELECTOR) ?? [])
    if (items.length === 0) return
    const idx = items.indexOf(document.activeElement as HTMLElement)
    let next = -1
    if (e.key === 'ArrowDown') next = idx < 0 ? 0 : (idx + 1) % items.length
    else if (e.key === 'ArrowUp') next = idx < 0 ? items.length - 1 : (idx - 1 + items.length) % items.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = items.length - 1
    else if (e.key === 'Tab') {
      e.preventDefault()
      rest.onClose()
      return
    }
    if (next >= 0) {
      e.preventDefault()
      items[next].focus()
    }
  }

  return (
    <Popover
      {...rest}
      open={open}
      padded={false}
      role="menu"
      className={clsx('ui-menu', className)}
      floatingRef={ref}
      floatingProps={{ onKeyDown, 'aria-label': ariaLabel } as React.HTMLAttributes<HTMLDivElement>}
    >
      {children}
    </Popover>
  )
}

export interface MenuItemProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onSelect'> {
  icon?: ReactNode
  /** Right-aligned hint, e.g. a shortcut. */
  hint?: ReactNode
  danger?: boolean
  /** Renders as a checkable item (role="menuitemcheckbox"). */
  checked?: boolean
  onSelect?: () => void
}

export function MenuItem({ icon, hint, danger, checked, onSelect, onClick, className, children, ...rest }: MenuItemProps) {
  return (
    <button
      type="button"
      role={checked === undefined ? 'menuitem' : 'menuitemcheckbox'}
      aria-checked={checked}
      tabIndex={-1}
      className={clsx('ui-menu-item', danger && 'ui-menu-item--danger', className)}
      onClick={(e) => {
        onClick?.(e)
        if (!e.defaultPrevented) onSelect?.()
      }}
      {...rest}
    >
      {icon}
      <span className="ui-menu-item-label">{children}</span>
      {hint != null && <span className="ui-menu-item-hint">{hint}</span>}
    </button>
  )
}

export function MenuSeparator() {
  return <div role="separator" className="ui-menu-separator" />
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className="ui-menu-label">{children}</div>
}
