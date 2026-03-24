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

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            try {
              const data = JSON.parse(line.slice(6)) as SSEEvent
              onEvent(data)
              if (data.type === 'done') {
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
