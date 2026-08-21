import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import type { ExtensionSlotName } from '@pando/client/services/extensionUI'
import ExtensionPanel from './ExtensionPanel'

interface Props {
  slot: ExtensionSlotName
  /** Restrict the slot to a single panel, used by the sidebar's panel route. */
  panelId?: string
}

/**
 * Renders every extension panel registered for a slot.
 *
 * On a standard build there are none and this renders nothing at all, which is
 * why it can be dropped into a layout unconditionally.
 */
export default function ExtensionSlot({ slot, panelId }: Props) {
  const panels = useExtensionPanelsStore((s) => s.panels)
  const forSlot = panels.filter((p) => p.slot === slot && (!panelId || p.id === panelId))

  if (forSlot.length === 0) return null

  return (
    <>
      {forSlot.map((p) => (
        <ExtensionPanel key={p.id} panel={p} />
      ))}
    </>
  )
}
