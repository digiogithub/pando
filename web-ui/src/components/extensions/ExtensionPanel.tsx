import { useEffect, useRef, useState } from 'react'
import type { ExtensionPanel as PanelManifest } from '@pando/client/services/extensionUI'
import { panelContext, type PanelModule } from '@/lib/pandoUI'

interface Props {
  panel: PanelManifest
}

/**
 * Mounts one extension panel into a DOM node of its own.
 *
 * Everything here is defensive on purpose. The module is third-party code
 * loaded at run time from a build we did not make: a panel that throws on
 * import or on mount must degrade to a visible message in its own box and
 * leave the rest of the UI working.
 */
export default function ExtensionPanel({ panel }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const el = hostRef.current
    if (!el) return

    let cancelled = false
    let cleanup: (() => void) | void

    void (async () => {
      try {
        // @vite-ignore: the URL is only known at run time, from the manifest.
        const mod = (await import(/* @vite-ignore */ panel.entry)) as PanelModule
        if (cancelled) return
        if (typeof mod.default !== 'function') {
          setError(`panel ${panel.id} has no default export`)
          return
        }
        cleanup = await mod.default(el, panelContext(panel.id, panel.extension))
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      }
    })()

    return () => {
      cancelled = true
      try {
        cleanup?.()
      } catch {
        // A panel that fails to clean up must not break the unmount of the
        // screen it lived on.
      }
    }
  }, [panel.entry, panel.id, panel.extension])

  if (error) {
    return (
      <div style={{ padding: '0.5rem', fontSize: 12, color: 'var(--error)' }}>
        {panel.title || panel.id}: {error}
      </div>
    )
  }

  return <div ref={hostRef} data-extension-panel={panel.id} />
}
