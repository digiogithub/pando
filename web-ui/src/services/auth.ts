import api from './api'

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

export async function checkHealth(): Promise<boolean> {
  try {
    await fetch('/health')
    return true
  } catch {
    return false
  }
}
