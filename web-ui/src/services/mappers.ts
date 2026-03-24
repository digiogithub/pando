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

interface RawPart {
  text?: string
  reason?: string
  time?: number
  tool_name?: string
  tool_input?: Record<string, unknown>
  tool_result?: string
  image_url?: string
}

interface RawMessage {
  ID: string
  SessionID: string
  Role: 'user' | 'assistant' | 'system'
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

function mapParts(parts: RawPart[]): ContentPart[] {
  return parts
    .filter((p) => p.text != null || p.tool_name != null || p.image_url != null)
    .map((p) => {
      if (p.tool_name) {
        return {
          type: 'tool_call' as const,
          tool_name: p.tool_name,
          tool_input: p.tool_input,
          tool_result: p.tool_result,
        }
      }
      if (p.image_url) {
        return { type: 'image' as const, image_url: p.image_url }
      }
      return { type: 'text' as const, text: p.text ?? '' }
    })
}

export function mapMessage(raw: RawMessage): Message {
  return {
    id: raw.ID,
    session_id: raw.SessionID,
    role: raw.Role,
    content: mapParts(raw.Parts ?? []),
    model: raw.Model || undefined,
    created_at: unixToISO(raw.CreatedAt),
  }
}
