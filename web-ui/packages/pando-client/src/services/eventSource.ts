import api, { getBaseURL } from './api'

/**
 * Opens an EventSource on an API path. EventSource cannot send headers, so the
 * API token travels as the ?token= query parameter the server also accepts.
 */
export function openEventSource(path: string): EventSource {
  const token = api.getToken()
  const url = `${getBaseURL()}${path}`
  return new EventSource(token ? `${url}${url.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}` : url)
}

/** Parses an SSE data payload, or returns undefined when it is malformed. */
export function parseEventData<T>(evt: MessageEvent): T | undefined {
  try {
    return JSON.parse(evt.data as string) as T
  } catch {
    return undefined
  }
}
