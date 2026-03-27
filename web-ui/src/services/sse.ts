import api from './api'
import type { SSEEvent, SSEToolResult } from '@/types'

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
              const eventType = (currentEventType || 'content') as SSEEvent['type']
              const event: SSEEvent = {
                type: eventType,
                session_id: typeof raw.sessionId === 'string' ? raw.sessionId : undefined,
                content: typeof raw.text === 'string' ? raw.text : undefined,
                error: typeof raw.error === 'string' ? raw.error : undefined,
                tool_call: raw.id && raw.name
                  ? { id: raw.id as string, name: raw.name as string, input: (raw.input as string) ?? '' }
                  : undefined,
                tool_result: raw.tool_call_id
                  ? {
                      tool_call_id: raw.tool_call_id as string,
                      name: (raw.name as string) ?? '',
                      content: (raw.content as string) ?? '',
                      is_error: Boolean(raw.is_error),
                    } as SSEToolResult
                  : undefined,
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
