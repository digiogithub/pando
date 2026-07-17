import { useEffect, useState } from 'react'
import { useSessionStore } from '@/stores/sessionStore'

/** Idle glyph: the Pando tree, shown whenever no run is in flight. */
export const LOGO_IDLE_GLYPH = '木'

/**
 * Glyphs cycled while the agent (or one of its subagents) is working. They spell
 * out the growth of a pando colony: root, branch, leaf, grove, dense forest.
 */
export const LOGO_ANIM_FRAMES = ['本', '枝', '葉', '林', '森']

const MIN_INTERVAL_MS = 2000
const MAX_INTERVAL_MS = 3000

/**
 * useAnimatedLogo returns the glyph the header logo must render right now. While
 * any session has a live run it swaps to a random growth frame every 2-3s;
 * otherwise it rests on the idle Pando tree.
 */
export function useAnimatedLogo(): string {
  const isStreaming = useSessionStore((s) => s.isStreaming)
  const anyRunning = useSessionStore((s) => s.sessions.some((sess) => sess.is_running))
  const busy = isStreaming || anyRunning
  const [frame, setFrame] = useState(-1)

  useEffect(() => {
    if (!busy) {
      setFrame(-1)
      return
    }
    let timer: ReturnType<typeof setTimeout>
    const schedule = () => {
      const delay = MIN_INTERVAL_MS + Math.random() * (MAX_INTERVAL_MS - MIN_INTERVAL_MS)
      timer = setTimeout(() => {
        // Draw from the frames other than the current one so every tick is visible.
        setFrame((prev) => {
          if (prev < 0) return Math.floor(Math.random() * LOGO_ANIM_FRAMES.length)
          let next = Math.floor(Math.random() * (LOGO_ANIM_FRAMES.length - 1))
          if (next >= prev) next++
          return next
        })
        schedule()
      }, delay)
    }
    schedule()
    return () => clearTimeout(timer)
  }, [busy])

  return frame >= 0 && frame < LOGO_ANIM_FRAMES.length ? LOGO_ANIM_FRAMES[frame] : LOGO_IDLE_GLYPH
}
