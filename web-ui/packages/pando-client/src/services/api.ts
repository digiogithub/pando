import { localBrowserStorage } from './storage'
import type { ServerInfo } from '../types'

const TOKEN_KEY = 'pando_token'
const CHILD_MODE_RESTRICTED_PATHS = new Set(['/projects', '/instances'])

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
  if (typeof window !== 'undefined' && window.__PANDO_API_BASE__) {
    return window.__PANDO_API_BASE__
  }
  if (import.meta.env.VITE_API_BASE_URL) {
    return import.meta.env.VITE_API_BASE_URL as string
  }
  return ''
}

function normalizeBaseURL(url: string): string {
  const trimmed = url.trim()
  if (!trimmed || trimmed === '/') return ''

  try {
    const parsed = new URL(trimmed)
    const pathname = parsed.pathname.replace(/\/+$/, '') || '/'
    return `${parsed.origin}${pathname === '/' ? '' : pathname}`
  } catch {
    return trimmed.replace(/\/+$/, '')
  }
}

let baseURL = normalizeBaseURL(resolveInitialBaseURL())
let serverReportedProjectChildMode = false

function getConfiguredBasePath(url: string): string {
  const trimmed = url.trim()
  if (!trimmed) return ''

  try {
    const parsed = new URL(trimmed, 'http://pando.local')
    return (parsed.pathname.replace(/\/+$/, '') || '/')
  } catch {
    const normalized = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
    return normalized.replace(/\/+$/, '') || '/'
  }
}

export function setBaseURL(url: string): void {
  baseURL = normalizeBaseURL(url)
}

export function getBaseURL(): string {
  return baseURL
}

export function isProjectChildMode(): boolean {
  return serverReportedProjectChildMode || /^\/api\/v1\/projects\/[^/]+\/web$/.test(getConfiguredBasePath(baseURL))
}

export function setServerProjectChildMode(startupMode: string | null | undefined): void {
  serverReportedProjectChildMode = startupMode === 'project-child'
}

export function isChildModeRestrictedPath(path: string): boolean {
  const normalized = (path.replace(/\/+$/, '') || '/')
  return CHILD_MODE_RESTRICTED_PATHS.has(normalized)
}

export async function fetchServerInfo(): Promise<ServerInfo> {
  const response = await fetch(resolveAPIURL('/health'))
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`)
  }
  const payload = await response.json() as ServerInfo
  setServerProjectChildMode(payload.startup_mode)
  return payload
}

export function resolveAPIURL(path: string): string {
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(path)) {
    return path
  }
  if (!baseURL) {
    return path
  }
  return path.startsWith('/') ? `${baseURL}${path}` : `${baseURL}/${path}`
}

export function initDesktopMode(config: { apiBase: string; token: string }): void {
  setBaseURL(config.apiBase)
  if (config.token) {
    setToken(config.token)
  }
}

function getToken(): string | null {
  return localBrowserStorage.getItem(TOKEN_KEY)
}

function setToken(token: string): void {
  localBrowserStorage.setItem(TOKEN_KEY, token)
}

function removeToken(): void {
  localBrowserStorage.removeItem(TOKEN_KEY)
}

interface FetchOptions extends RequestInit {
  skipAuth?: boolean
}

// Raised when the server is exposed off-localhost and demands basic-auth
// credentials. Callers show the login dialog instead of wiping the token, which
// would only trigger a reload loop since no token can be obtained without
// credentials in the first place.
export class BasicAuthRequiredError extends Error {
  constructor() {
    super('basic_auth_required')
    this.name = 'BasicAuthRequiredError'
  }
}

async function fetchApi<T>(path: string, options: FetchOptions = {}): Promise<T> {
  const { skipAuth, ...init } = options
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    // Marks the caller as the SPA so the server skips the WWW-Authenticate
    // challenge and the browser's native credential prompt never appears.
    'X-Pando-Client': 'web',
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
    response = await fetch(resolveAPIURL(path), { ...init, headers })
  } catch (err) {
    // Network-level failure (server unreachable) — notify handler immediately
    _networkErrorHandler?.()
    throw err
  }

  if (response.status === 401) {
    if ((await response.clone().text()).includes('basic_auth_required')) {
      throw new BasicAuthRequiredError()
    }
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
  post: <T>(path: string, body: unknown, options: FetchOptions = {}) =>
    fetchApi<T>(path, { ...options, method: 'POST', body: JSON.stringify(body) }),
  patch: <T>(path: string, body: unknown) =>
    fetchApi<T>(path, { method: 'PATCH', body: JSON.stringify(body) }),
  put: <T>(path: string, body: unknown) =>
    fetchApi<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
  delete: <T>(path: string) => fetchApi<T>(path, { method: 'DELETE' }),
  getToken,
  setToken,
  removeToken,
}

export default api
