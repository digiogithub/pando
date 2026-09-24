/** Tiny anchored-positioning helper shared by Popover, Menu and Tooltip. */

export type Placement =
  | 'top'
  | 'bottom'
  | 'top-start'
  | 'top-end'
  | 'bottom-start'
  | 'bottom-end'
  | 'left'
  | 'right'

const MARGIN = 8

/**
 * Returns viewport (position: fixed) coordinates for a floating element of
 * `size` next to `anchor`, flipping to the opposite side when it would
 * overflow and clamping inside the viewport.
 */
export function computePosition(
  anchor: DOMRect,
  size: { width: number; height: number },
  placement: Placement,
  offset = 6,
): { top: number; left: number; placement: Placement } {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const [initialSide, align] = placement.split('-') as [string, string | undefined]
  let side = initialSide

  if (side === 'bottom' && anchor.bottom + offset + size.height > vh - MARGIN && anchor.top - offset - size.height >= MARGIN) side = 'top'
  else if (side === 'top' && anchor.top - offset - size.height < MARGIN && anchor.bottom + offset + size.height <= vh - MARGIN) side = 'bottom'
  else if (side === 'right' && anchor.right + offset + size.width > vw - MARGIN) side = 'left'
  else if (side === 'left' && anchor.left - offset - size.width < MARGIN) side = 'right'

  let top: number
  let left: number
  if (side === 'top' || side === 'bottom') {
    top = side === 'bottom' ? anchor.bottom + offset : anchor.top - offset - size.height
    if (align === 'start') left = anchor.left
    else if (align === 'end') left = anchor.right - size.width
    else left = anchor.left + anchor.width / 2 - size.width / 2
  } else {
    left = side === 'right' ? anchor.right + offset : anchor.left - offset - size.width
    top = anchor.top + anchor.height / 2 - size.height / 2
  }

  left = Math.max(MARGIN, Math.min(left, vw - size.width - MARGIN))
  top = Math.max(MARGIN, Math.min(top, vh - size.height - MARGIN))
  return { top, left, placement: (align ? `${side}-${align}` : side) as Placement }
}

export const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]'
