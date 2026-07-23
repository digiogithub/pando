import api, { notifyNetworkError } from './api'
import type { SSEEvent } from '@/types'

type ToolCallKind = NonNullable<NonNullable<SSEEvent['tool_call']>['kind']>
type ToolCallStatus = NonNullable<NonNullable<SSEEvent['tool_call']>['status']>
type ToolResultTerminal = NonNullable<NonNullable<SSEEvent['tool_result']>['terminal']>
type ToolResultDiff = NonNullable<NonNullable<SSEEvent['tool_result']>['diff']>
type PlanEntry = NonNullable<SSEEvent['plan_entries']>[number]

function isToolCallKind(value: unknown): value is ToolCallKind {
  return typeof value === 'string' && ['execute', 'edit', 'read', 'search', 'fetch', 'think', 'switch_mode', 'other'].includes(value)
}

function isToolCallStatus(value: unknown): value is ToolCallStatus {
  return typeof value === 'string' && ['pending', 'in_progress', 'completed', 'failed'].includes(value)
}

function isToolResultTerminal(value: unknown): value is ToolResultTerminal {
  return typeof value === 'object'
    && value !== null
    && typeof (value as Record<string, unknown>).terminal_id === 'string'
    && typeof (value as Record<string, unknown>).exit_code === 'number'
}

function isToolResultDiff(value: unknown): value is ToolResultDiff {
  return typeof value === 'object'
    && value !== null
    && typeof (value as Record<string, unknown>).file_path === 'string'
}

/** Shared SSE body reader used by both POST and GET stream functions. */
async function consumeSSEBody(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: SSEEvent) => void,
  onDone?: () => void,
): Promise<void> {
  const reader = body.getReader()
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
          const eventType = (currentEventType || 'content') as SSEEvent['type']
          const event = parseSSEPayload(eventType, raw)
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
}

/**
 * Connect to an existing background session stream via GET (no body).
 * Used to reconnect to a session that is already running in the background.
 * Events are replayed from the server-side buffer then continue live.
 */
export function createGETSSEStream(
  url: string,
  onEvent: (event: SSEEvent) => void,
  onError?: (error: Error) => void,
  onDone?: () => void,
): AbortController {
  const controller = new AbortController()
  const token = api.getToken()

  fetch(url, {
    method: 'GET',
    headers: {
      ...(token ? { 'X-Pando-Token': token } : {}),
    },
    signal: controller.signal,
  })
    .then(async (response) => {
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      if (!response.body) throw new Error('No response body')
      await consumeSSEBody(response.body, onEvent, onDone)
    })
    .catch((err: unknown) => {
      if (err instanceof Error && err.name !== 'AbortError') {
        if (err instanceof TypeError) notifyNetworkError()
        onError?.(err)
      }
    })

  return controller
}

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
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      if (!response.body) throw new Error('No response body')
      await consumeSSEBody(response.body, onEvent, onDone)
    })
    .catch((err: unknown) => {
      if (err instanceof Error && err.name !== 'AbortError') {
        // Network-level failure (server unreachable) — notify server store immediately
        if (err instanceof TypeError) {
          notifyNetworkError()
        }
        onError?.(err)
      }
    })

  return controller
}

function parseSSEPayload(eventType: SSEEvent['type'], raw: Record<string, unknown>): SSEEvent {
  const base: SSEEvent = {
    type: eventType,
    session_id: typeof raw.sessionId === 'string' ? raw.sessionId : undefined,
    content: typeof raw.text === 'string' ? raw.text : undefined,
    error: typeof raw.error === 'string' ? raw.error : undefined,
  }

  switch (eventType) {
    case 'tool_call':
      if (raw.id && raw.name) {
        base.tool_call = {
          id: raw.id as string,
          name: raw.name as string,
          input: (raw.input as string) ?? '',
          kind: isToolCallKind(raw.kind) ? raw.kind : undefined,
          title: typeof raw.title === 'string' ? raw.title : undefined,
          status: isToolCallStatus(raw.status) ? raw.status : undefined,
          locations: Array.isArray(raw.locations) ? raw.locations : undefined,
        }
      }
      break

    case 'tool_call_update':
      if (raw.id) {
        base.tool_call_update = {
          id: raw.id as string,
          status: isToolCallStatus(raw.status) ? raw.status : undefined,
          kind: isToolCallKind(raw.kind) ? raw.kind : undefined,
          title: typeof raw.title === 'string' ? raw.title : undefined,
          input: typeof raw.input === 'string' ? raw.input : undefined,
          locations: Array.isArray(raw.locations) ? raw.locations : undefined,
        }
      }
      break

    case 'tool_result':
      if (raw.tool_call_id) {
        base.tool_result = {
          tool_call_id: raw.tool_call_id as string,
          name: (raw.name as string) ?? '',
          content: (raw.content as string) ?? '',
          is_error: Boolean(raw.is_error),
          kind: isToolCallKind(raw.kind) ? raw.kind : undefined,
          title: typeof raw.title === 'string' ? raw.title : undefined,
          status: isToolCallStatus(raw.status) ? raw.status : undefined,
          locations: Array.isArray(raw.locations) ? raw.locations : undefined,
          images: Array.isArray(raw.images) ? raw.images.filter((i): i is string => typeof i === 'string') : undefined,
          raw_output: typeof raw.raw_output === 'object' && raw.raw_output !== null
            ? raw.raw_output as Record<string, unknown> : undefined,
          terminal: isToolResultTerminal(raw.terminal) ? raw.terminal : undefined,
          diff: isToolResultDiff(raw.diff) ? raw.diff : undefined,
        }
      }
      break

    case 'plan_update':
      if (Array.isArray(raw.entries)) {
        base.plan_entries = raw.entries
          .filter((entry): entry is PlanEntry => typeof entry === 'object' && entry !== null && typeof entry.title === 'string' && typeof entry.status === 'string')
      }
      break

    case 'goal_status':
      if (typeof raw.goal === 'object' && raw.goal !== null) {
        const goal = raw.goal as Record<string, unknown>
        if (typeof goal.objective === 'string' && typeof goal.status === 'string') {
          base.goal = {
            objective: goal.objective,
            status: goal.status,
            iteration: typeof goal.iteration === 'number' ? goal.iteration : 0,
            maxIterations: typeof goal.maxIterations === 'number' ? goal.maxIterations : 0,
            progress: typeof goal.progress === 'string' ? goal.progress : undefined,
            nextStep: typeof goal.nextStep === 'string' ? goal.nextStep : undefined,
            startedAt: typeof goal.startedAt === 'number' ? goal.startedAt : 0,
            completedAt: typeof goal.completedAt === 'number' ? goal.completedAt : undefined,
          }
        }
      }
      break

    case 'token_usage':
      base.session_id = typeof raw.session_id === 'string' ? raw.session_id : base.session_id
      base.token_usage = {
        prompt_tokens: typeof raw.prompt_tokens === 'number' ? raw.prompt_tokens : 0,
        completion_tokens: typeof raw.completion_tokens === 'number' ? raw.completion_tokens : 0,
        context_window: typeof raw.context_window === 'number' ? raw.context_window : 0,
        estimated: Boolean(raw.estimated),
      }
      break

    case 'permission_request':
      if (typeof raw.id === 'string' && typeof raw.tool_name === 'string') {
        base.session_id = typeof raw.session_id === 'string' ? raw.session_id : base.session_id
        base.permission_request = {
          id: raw.id,
          session_id: typeof raw.session_id === 'string' ? raw.session_id : '',
          tool_name: raw.tool_name,
          description: typeof raw.description === 'string' ? raw.description : undefined,
          action: typeof raw.action === 'string' ? raw.action : undefined,
          path: typeof raw.path === 'string' ? raw.path : undefined,
          params: raw.params,
        }
      }
      break

    case 'question_request':
      if (typeof raw.id === 'string' && Array.isArray(raw.questions)) {
        base.session_id = typeof raw.session_id === 'string' ? raw.session_id : base.session_id
        base.question_request = {
          id: raw.id,
          session_id: typeof raw.session_id === 'string' ? raw.session_id : '',
          questions: (raw.questions as Record<string, unknown>[]).map((q) => ({
            id: typeof q.id === 'string' ? q.id : '',
            header: typeof q.header === 'string' ? q.header : '',
            question: typeof q.question === 'string' ? q.question : '',
            multi_select: Boolean(q.multi_select),
            options: Array.isArray(q.options)
              ? (q.options as Record<string, unknown>[]).map((o) => ({
                  label: typeof o.label === 'string' ? o.label : '',
                  description: typeof o.description === 'string' ? o.description : '',
                }))
              : [],
          })),
        }
      }
      break

    case 'steering_queued':
    case 'steering_injected':
      base.session_id = typeof raw.session_id === 'string' ? raw.session_id : base.session_id
      base.message = typeof raw.message === 'string' ? raw.message : undefined
      break

    default:
      // session, content, content_delta, thinking_delta, todos_update, error, done
      // — already handled by base fields
      break
  }

  return base
}
