import api, { getBaseURL } from './api'
import { isDesktop } from './desktop'

interface TokenResponse {
  token: string
}

export async function authenticate(): Promise<string> {
  const existing = api.getToken()
  if (existing) return existing

  // In desktop mode the token is injected via initDesktopMode — if it's already
  // set we never reach here. If somehow we do, skip the HTTP call.
  if (isDesktop) return ''

  const data = await api.post<TokenResponse>('/api/v1/token', {})
  api.setToken(data.token)
  return data.token
}

export async function checkHealth(): Promise<boolean> {
  // In desktop mode the Go process owns the server lifecycle — always healthy.
  if (isDesktop) return true

  try {
    // Use configured base URL when set (e.g. after backend HTML injection or dev mode env var).
    // Falls back to relative path when served from the same origin.
    const base = getBaseURL()
    await fetch(base + '/health')
    return true
  } catch {
    return false
  }
}
