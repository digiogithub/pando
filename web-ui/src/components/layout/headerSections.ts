export const HEADER_SECTION_KEYS: Record<string, string> = {
  '': 'nav.chat',
  chat: 'nav.chat',
  projects: 'nav.projects',
  orchestrator: 'nav.orchestrator',
  evaluator: 'nav.selfImprovement',
  snapshots: 'nav.agentVcs',
  logs: 'nav.logs',
  editor: 'nav.codeEditor',
  terminal: 'nav.terminal',
  settings: 'nav.settings',
  design: 'nav.design',
  instances: 'nav.instances',
}

export function resolveHeaderSection(
  pathname: string,
  t: (key: string) => string,
  extensionPanels?: Array<{ id: string; title: string }>,
): string {
  const segments = pathname.split('/').filter(Boolean)
  const first = segments[0] ?? ''
  if (first === 'ext' && segments[1]) {
    const panel = extensionPanels?.find((entry) => entry.id === segments[1])
    return panel?.title || segments[1]
  }
  return HEADER_SECTION_KEYS[first] ? t(HEADER_SECTION_KEYS[first]) : ''
}
