const FALLBACK_ORIGIN = 'http://pando.local'

function resolveConfiguredBaseURL(): string {
  if (typeof window !== 'undefined' && window.__PANDO_API_BASE__) {
    return window.__PANDO_API_BASE__
  }
  if (import.meta.env.VITE_API_BASE_URL) {
    return import.meta.env.VITE_API_BASE_URL as string
  }
  return ''
}

function normalizeBasePath(baseURL: string): string {
  const trimmed = baseURL.trim()
  if (!trimmed) return ''

  try {
    const parsed = new URL(trimmed, FALLBACK_ORIGIN)
    const pathname = parsed.pathname.replace(/\/+$/, '') || '/'
    return pathname === '/' ? '' : pathname
  } catch {
    const withoutQuery = trimmed.split(/[?#]/, 1)[0] ?? ''
    const normalized = withoutQuery.startsWith('/') ? withoutQuery : `/${withoutQuery}`
    const pathname = normalized.replace(/\/+$/, '') || '/'
    return pathname === '/' ? '' : pathname
  }
}

function shortStableHash(value: string): string {
  let hash = 0x811c9dc5

  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }

  return (hash >>> 0).toString(16).padStart(8, '0')
}

export function storageKey(name: string): string {
  const basePath = normalizeBasePath(resolveConfiguredBaseURL())
  return basePath ? `${name}@${shortStableHash(basePath)}` : name
}

interface SafeBrowserStorage {
  getItem: (name: string) => string | null
  setItem: (name: string, value: string) => void
  removeItem: (name: string) => void
}

function makeSafeStorage(getStorage: () => Storage | undefined): SafeBrowserStorage {
  return {
    getItem(name) {
      try {
        return getStorage()?.getItem(storageKey(name)) ?? null
      } catch {
        return null
      }
    },
    setItem(name, value) {
      try {
        getStorage()?.setItem(storageKey(name), value)
      } catch {
        // Storage may be unavailable or quota-limited; treat persistence as best effort.
      }
    },
    removeItem(name) {
      try {
        getStorage()?.removeItem(storageKey(name))
      } catch {
        // Storage may be unavailable or quota-limited; treat persistence as best effort.
      }
    },
  }
}

export const localBrowserStorage = makeSafeStorage(() =>
  typeof window !== 'undefined' ? window.localStorage : undefined,
)

export const sessionBrowserStorage = makeSafeStorage(() =>
  typeof window !== 'undefined' ? window.sessionStorage : undefined,
)
