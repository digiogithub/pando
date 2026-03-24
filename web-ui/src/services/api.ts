const TOKEN_KEY = 'pando_token'

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

  const response = await fetch(path, { ...init, headers })

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
