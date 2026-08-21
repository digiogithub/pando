import { useParams } from 'react-router-dom'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import NotFound from '@/components/shared/NotFound'
import ExtensionPanel from './ExtensionPanel'

/**
 * The page behind a sidebar panel's route (/ext/:panelId).
 *
 * A panel that is not in the manifest is a 404 rather than an empty page: the
 * link can only come from a stale bookmark or a build that no longer contains
 * the extension, and both deserve the same honest answer.
 */
export default function ExtensionPanelPage() {
  const { panelId } = useParams<{ panelId: string }>()
  const panels = useExtensionPanelsStore((s) => s.panels)
  const loaded = useExtensionPanelsStore((s) => s.loaded)

  const panel = panels.find((p) => p.id === panelId && p.slot === 'sidebar')
  if (!panel) return loaded ? <NotFound /> : null

  return (
    <div style={{ padding: '1rem', height: '100%', overflow: 'auto' }}>
      <ExtensionPanel panel={panel} />
    </div>
  )
}
