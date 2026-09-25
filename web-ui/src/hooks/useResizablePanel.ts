import { useCallback, useEffect, useState, type RefObject } from 'react'

function readStoredWidth(key: string | undefined): number | null {
  if (!key) return null
  try {
    const raw = localStorage.getItem(key)
    const n = raw ? Number(raw) : NaN
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

function writeStoredWidth(key: string | undefined, width: number) {
  if (!key) return
  try {
    localStorage.setItem(key, String(Math.round(width)))
  } catch {
    // storage unavailable (private mode, blocked site data) — width just won't persist
  }
}

/**
 * Width state for a right-docked side panel that the user can drag-resize.
 * The max width is bounded by the container so the main pane always keeps
 * `minMain` pixels; the chosen width is persisted under `storageKey`.
 */
export function useResizablePanel({
  containerRef,
  defaultWidth,
  minWidth = 280,
  minMain = 240,
  storageKey,
}: {
  containerRef: RefObject<HTMLElement | null>
  defaultWidth: number
  minWidth?: number
  minMain?: number
  storageKey?: string
}) {
  const [width, setWidth] = useState(() => readStoredWidth(storageKey) ?? defaultWidth)

  const clamp = useCallback(
    (w: number) => {
      const containerWidth = containerRef.current?.clientWidth ?? Infinity
      const max = Math.max(minWidth, containerWidth - minMain)
      return Math.round(Math.min(max, Math.max(minWidth, w)))
    },
    [containerRef, minWidth, minMain],
  )

  const commit = useCallback(
    (w: number) => {
      const next = clamp(w)
      setWidth(next)
      writeStoredWidth(storageKey, next)
    },
    [clamp, storageKey],
  )

  // Clamp the stored width on mount and whenever the window shrinks, so the
  // panel never swallows the main pane.
  useEffect(() => {
    const onResize = () => setWidth((w) => clamp(w))
    onResize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [clamp])

  return { width, setWidth: commit, minWidth }
}
