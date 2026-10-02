import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'
import { useSessionModelStore } from '@pando/client/stores/sessionModelStore'

/** Human label for a model id: "claude-sonnet-4-6" → "Claude Sonnet 4.6", "copilot.gpt-4o" → "Copilot GPT-4o". */
export function formatModel(id: string): string {
  if (!id) return ''
  if (id.startsWith('copilot.')) return 'Copilot ' + formatModel(id.slice(8))
  if (id.startsWith('claude-')) {
    const rest = id.slice(7)
    const dash = rest.indexOf('-')
    if (dash === -1) return 'Claude ' + rest.charAt(0).toUpperCase() + rest.slice(1)
    const name = rest.slice(0, dash)
    const version = rest.slice(dash + 1).replace(/-/g, '.')
    return 'Claude ' + name.charAt(0).toUpperCase() + name.slice(1) + ' ' + version
  }
  if (id.startsWith('gpt-')) return 'GPT-' + id.slice(4)
  if (id.startsWith('gemini-')) return 'Gemini ' + id.slice(7)
  const slash = id.lastIndexOf('/')
  return slash >= 0 ? id.slice(slash + 1) : id
}

/**
 * Label of the active model selection. In model auto mode it is "Auto", plus
 * the model the last prompt was routed to once one is known ("Auto → model");
 * the route id and probability stay in the per-prompt routing notice.
 */
export function activeModelLabel(defaultModel: string, autoSelected: boolean, lastRoutedModel: string | null): string {
  if (!autoSelected) return formatModel(defaultModel)
  return lastRoutedModel ? `Auto → ${formatModel(lastRoutedModel)}` : 'Auto'
}

/**
 * Model selection in force for the active session: its own selection when it
 * has one, otherwise the configured default model and auto mode.
 */
export function useActiveModelSelection(): { model: string; autoSelected: boolean } {
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const globalAuto = useModelAutoModeStore((s) => s.autoSelected)
  const selection = useSessionModelStore((s) => s.selection)
  return {
    model: selection?.model ?? defaultModel,
    autoSelected: selection ? selection.auto : globalAuto,
  }
}

export function useActiveModelLabel(): string {
  const { model, autoSelected } = useActiveModelSelection()
  const lastRoutedModel = useModelAutoModeStore((s) => s.lastRoutedModel)
  return activeModelLabel(model, autoSelected, lastRoutedModel)
}
