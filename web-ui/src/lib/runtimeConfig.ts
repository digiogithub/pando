function normalizeRouterBasename(value: string | undefined): string {
  const trimmed = value?.trim()
  if (!trimmed || trimmed === '/') return '/'
  const normalized = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  return normalized.replace(/\/+$/, '')
}

export function getRouterBasename(): string {
  return normalizeRouterBasename(window.__PANDO_ROUTER_BASENAME__)
}

export function isRootRouterBasename(): boolean {
  return getRouterBasename() === '/'
}
