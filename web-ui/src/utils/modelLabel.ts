import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'

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

export function useActiveModelLabel(): string {
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const autoSelected = useModelAutoModeStore((s) => s.autoSelected)
  const lastRoutedModel = useModelAutoModeStore((s) => s.lastRoutedModel)
  return activeModelLabel(defaultModel, autoSelected, lastRoutedModel)
}
