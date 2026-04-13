import type { Session, Message, ContentPart } from '@/types'

// Backend returns PascalCase + Unix timestamps; frontend expects snake_case + ISO strings

interface RawSession {
  ID: string
  ParentSessionID?: string
  Title: string
  MessageCount: number
  PromptTokens: number
  CompletionTokens: number
  Cost: number
  CreatedAt: number
  UpdatedAt: number
}

// RawPart matches what the Go message.Message serializes to JSON via encoding/json.
// The backend stores parts as a typed-wrapper in the DB, but the API returns the
// Go structs directly. The concrete fields per type are:
//
//   TextContent      → { text }
//   ReasoningContent → { thinking }
//   ToolCall         → { id, name, input (JSON string), type, finished }
//   ToolResult       → { tool_call_id, name, content, metadata, is_error }
//   Finish           → { reason, time }
//   ImageURLContent  → { url, detail }
//
// NOTE: ToolResults are stored in a SEPARATE message with role="tool" following
// the assistant message that contains the ToolCall parts.
interface RawPart {
  // TextContent
  text?: string
  // ReasoningContent
  thinking?: string
  // Finish
  reason?: string
  time?: number
  // ToolCall (Go struct fields)
  id?: string
  name?: string
  input?: string       // JSON string of the tool input
  finished?: boolean
  // ToolResult (Go struct fields)
  tool_call_id?: string
  content?: string
  metadata?: string
  is_error?: boolean
  // ImageURLContent
  image_url?: string
  url?: string
  detail?: string
}

interface RawMessage {
  ID: string
  SessionID: string
  Role: 'user' | 'assistant' | 'system' | 'tool'
  Parts: RawPart[]
  Model?: string
  CreatedAt: number
  UpdatedAt?: number
}

function unixToISO(ts: number): string {
  return new Date(ts * 1000).toISOString()
}

export function mapSession(raw: RawSession): Session {
  return {
    id: raw.ID,
    parent_session_id: raw.ParentSessionID ?? '',
    title: raw.Title,
    message_count: raw.MessageCount,
    prompt_tokens: raw.PromptTokens,
    completion_tokens: raw.CompletionTokens,
    cost: raw.Cost,
    created_at: unixToISO(raw.CreatedAt),
    updated_at: unixToISO(raw.UpdatedAt),
  }
}

// Map parts of an assistant/user/system message.
// resultMap: pre-built map of tool_call_id → result, populated from 'tool' messages.
function mapParts(parts: RawPart[], resultMap: Map<string, { content: string; is_error: boolean }>): ContentPart[] {
  const out: ContentPart[] = []

  for (const p of parts) {
    // ── ToolCall: has `id` + `name` + `input` (JSON string)
    if (p.id && p.name !== undefined && p.input !== undefined) {
      let parsedInput: Record<string, unknown> | undefined
      try { parsedInput = JSON.parse(p.input) } catch { parsedInput = undefined }
      const result = resultMap.get(p.id)
      out.push({
        type: 'tool_call',
        tool_name: p.name,
        tool_call_id: p.id,
        tool_input: parsedInput,
        tool_result: result?.content,
        is_error: result?.is_error,
      })
      continue
    }

    // ── ToolResult standalone part (role="tool" message) — skip, already handled via resultMap
    if (p.tool_call_id) continue

    // ── ReasoningContent
    if (p.thinking != null) {
      out.push({ type: 'reasoning', text: p.thinking })
      continue
    }

    // ── ImageURLContent
    if (p.image_url || p.url) {
      out.push({ type: 'image', image_url: p.image_url ?? p.url })
      continue
    }

    // ── Finish — skip (no displayable content)
    if (p.reason != null && p.text == null) continue

    // ── TextContent
    if (p.text != null) {
      out.push({ type: 'text', text: p.text })
    }
  }

  return out
}

export function mapMessage(raw: RawMessage): Message {
  return mapMessageWithResults(raw, new Map())
}

function mapMessageWithResults(
  raw: RawMessage,
  resultMap: Map<string, { content: string; is_error: boolean }>,
): Message {
  return {
    id: raw.ID,
    session_id: raw.SessionID,
    role: raw.Role as 'user' | 'assistant' | 'system',
    content: mapParts(raw.Parts ?? [], resultMap),
    model: raw.Model || undefined,
    created_at: unixToISO(raw.CreatedAt),
  }
}

/**
 * Map a full list of messages, correlating ToolResult (role="tool") messages
 * with the ToolCall parts in their preceding assistant messages.
 * Tool messages are consumed and removed from the output.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapMessages(rawMessages: any[]): Message[] {
  const raws = rawMessages as RawMessage[]

  // Build a global map of tool_call_id → result from all role="tool" messages
  const globalResultMap = new Map<string, { content: string; is_error: boolean }>()
  for (const raw of raws) {
    if (raw.Role !== 'tool') continue
    for (const p of raw.Parts ?? []) {
      if (p.tool_call_id && p.content !== undefined) {
        globalResultMap.set(p.tool_call_id, {
          content: p.content,
          is_error: p.is_error ?? false,
        })
      }
    }
  }

  // Map non-tool messages, injecting results into ToolCall parts
  return raws
    .filter((raw) => raw.Role !== 'tool')
    .map((raw) => mapMessageWithResults(raw, globalResultMap))
}
