import api, { resolveAPIURL } from './api'

interface TokenResponse {
  token: string
}

export async function authenticate(): Promise<string> {
  const existing = api.getToken()
  if (existing) return existing

  const data = await api.post<TokenResponse>('/api/v1/token', {})
  api.setToken(data.token)
  return data.token
}

// basicAuthHeader base64-encodes credentials as UTF-8 bytes; btoa alone throws on
// any character outside Latin-1.
function basicAuthHeader(username: string, password: string): string {
  const bytes = new TextEncoder().encode(`${username}:${password}`)
  return 'Basic ' + btoa(String.fromCharCode(...bytes))
}

// authenticateWithCredentials exchanges basic-auth credentials for the API token.
// /api/v1/token sits behind the basic-auth gate, so obtaining a token is proof of
// authentication; every later call keeps using the token.
export async function authenticateWithCredentials(username: string, password: string): Promise<string> {
  const data = await api.post<TokenResponse>(
    '/api/v1/token',
    {},
    { skipAuth: true, headers: { Authorization: basicAuthHeader(username, password) } },
  )
  api.setToken(data.token)
  return data.token
}

export async function checkHealth(): Promise<boolean> {
  try {
    // Use configured base URL when set (e.g. after backend HTML injection or dev mode env var).
    // Falls back to relative path when served from the same origin.
    await fetch(resolveAPIURL('/health'))
    return true
  } catch {
    return false
  }
}
