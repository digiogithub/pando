import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'

const KEY_STEP = 24

/**
 * Vertical drag handle placed on the left edge of a right-docked panel.
 * Dragging left widens the panel; arrow keys resize it from the keyboard.
 */
export default function ResizeHandle({
  width,
  onResize,
  minWidth,
  label = 'Resize panel',
}: {
  width: number
  onResize: (width: number) => void
  minWidth?: number
  label?: string
}) {
  const drag = useRef<{ startX: number; startWidth: number } | null>(null)
  const [dragging, setDragging] = useState(false)

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    drag.current = { startX: e.clientX, startWidth: width }
    setDragging(true)
  }

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return
    onResize(drag.current.startWidth + (drag.current.startX - e.clientX))
  }

  const endDrag = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return
    drag.current = null
    setDragging(false)
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'ArrowLeft') onResize(width + KEY_STEP)
    else if (e.key === 'ArrowRight') onResize(width - KEY_STEP)
    else return
    e.preventDefault()
  }

  // Keep the resize cursor and suppress text selection page-wide while dragging.
  useEffect(() => {
    if (!dragging) return
    const { cursor, userSelect } = document.body.style
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    return () => {
      document.body.style.cursor = cursor
      document.body.style.userSelect = userSelect
    }
  }, [dragging])

  return (
    <div
      className="resize-handle"
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={width}
      aria-valuemin={minWidth}
      tabIndex={0}
      data-dragging={dragging || undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onKeyDown={onKeyDown}
    />
  )
}
