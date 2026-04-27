const TOKEN_KEY = 'pando_token'

// Network error handler — registered by serverStore to get immediate notification
// when the server is unreachable (TypeError: Failed to fetch).
let _networkErrorHandler: (() => void) | null = null
export function registerNetworkErrorHandler(cb: () => void): void {
  _networkErrorHandler = cb
}

export function notifyNetworkError(): void {
  _networkErrorHandler?.()
}

// Resolve initial base URL from injected runtime config or dev-mode env var.
// Priority: window.__PANDO_API_BASE__ (injected by backend) > VITE_API_BASE_URL (dev override) > '' (same origin)
function resolveInitialBaseURL(): string {
  if (typeof window !== 'undefined' && (window as Window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__) {
    return (window as Window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__!
  }
  if (import.meta.env.VITE_API_BASE_URL) {
    return import.meta.env.VITE_API_BASE_URL as string
  }
  return ''
}

let baseURL = resolveInitialBaseURL()

export function setBaseURL(url: string): void {
  baseURL = url
}

export function getBaseURL(): string {
  return baseURL
}

export function initDesktopMode(config: { apiBase: string; token: string }): void {
  setBaseURL(config.apiBase)
  if (config.token) {
    setToken(config.token)
  }
}

function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

function removeToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

interface FetchOptions extends RequestInit {
  skipAuth?: boolean
}

async function fetchApi<T>(path: string, options: FetchOptions = {}): Promise<T> {
  const { skipAuth, ...init } = options
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init.headers as Record<string, string>),
  }

  if (!skipAuth) {
    const token = getToken()
    if (token) {
      headers['X-Pando-Token'] = token
    }
  }

  let response: Response
  try {
    response = await fetch(baseURL + path, { ...init, headers })
  } catch (err) {
    // Network-level failure (server unreachable) — notify handler immediately
    _networkErrorHandler?.()
    throw err
  }

  if (response.status === 401) {
    const hadToken = !!getToken()
    removeToken()
    // Only reload if the user had a token that expired — avoid infinite loop on initial load
    if (hadToken) {
      window.location.reload()
    }
    throw new Error('Unauthorized')
  }

  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }

  const contentType = response.headers.get('Content-Type')
  if (contentType?.includes('application/json')) {
    return response.json() as Promise<T>
  }

  return response.text() as unknown as T
}

export const api = {
  get: <T>(path: string) => fetchApi<T>(path),
  post: <T>(path: string, body: unknown) =>
    fetchApi<T>(path, { method: 'POST', body: JSON.stringify(body) }),
  put: <T>(path: string, body: unknown) =>
    fetchApi<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
  delete: <T>(path: string) => fetchApi<T>(path, { method: 'DELETE' }),
  getToken,
  setToken,
  removeToken,
}

export default api
