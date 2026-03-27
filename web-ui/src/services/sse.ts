import api from './api'
import type { SSEEvent } from '@/types'

export function createSSEStream(
  url: string,
  body: unknown,
  onEvent: (event: SSEEvent) => void,
  onError?: (error: Error) => void,
  onDone?: () => void
): AbortController {
  const controller = new AbortController()
  const token = api.getToken()

  fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { 'X-Pando-Token': token } : {}),
    },
    body: JSON.stringify(body),
    signal: controller.signal,
  })
    .then(async (response) => {
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }
      if (!response.body) {
        throw new Error('No response body')
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let currentEventType = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (line.startsWith('event: ')) {
            currentEventType = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            try {
              const raw = JSON.parse(line.slice(6)) as Record<string, unknown>
              // Normalize backend field names to SSEEvent shape
              const event: SSEEvent = {
                type: (currentEventType || 'content') as SSEEvent['type'],
                session_id: typeof raw.sessionId === 'string' ? raw.sessionId : undefined,
                content: typeof raw.text === 'string' ? raw.text : undefined,
                error: typeof raw.error === 'string' ? raw.error : undefined,
              }
              currentEventType = ''
              onEvent(event)
              if (event.type === 'done') {
                onDone?.()
                return
              }
            } catch {
              // ignore malformed lines
            }
          }
        }
      }
      onDone?.()
    })
    .catch((err: unknown) => {
      if (err instanceof Error && err.name !== 'AbortError') {
        onError?.(err)
      }
    })

  return controller
}
