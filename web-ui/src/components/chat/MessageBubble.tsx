import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faRobot, faUser, faWrench, faChevronDown, faChevronRight,
  faBrain, faTerminal, faPen, faEye, faFolder, faGlobe,
  faFileLines, faMagnifyingGlass,
} from '@fortawesome/free-solid-svg-icons'
import type { IconDefinition } from '@fortawesome/fontawesome-svg-core'
import { format } from 'date-fns'
import 'highlight.js/styles/github-dark-dimmed.css'
import type { Message, ContentPart } from '@/types'
import type { StreamingState } from '@/hooks/useChat'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

// ─── Helpers ─────────────────────────────────────────────────────────────────

const labelStyle: React.CSSProperties = {
  fontSize: 10,
  fontWeight: 700,
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
  color: 'var(--fg-dim)',
  marginBottom: 3,
}

const codeBlockStyle: React.CSSProperties = {
  margin: 0,
  padding: '0.375rem 0.5rem',
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontFamily: 'monospace',
  fontSize: 11,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-all',
  lineHeight: 1.5,
}

// ─── Tool metadata detection ──────────────────────────────────────────────────

interface ToolMeta {
  icon: IconDefinition
  label: string
  summary: string
  accent: string
}

function getToolMeta(name: string, input?: Record<string, unknown> | null): ToolMeta {
  const n = name.toLowerCase()

  // bash / terminal
  if (n === 'bash' || n === 'execute_bash' || n === 'run_command') {
    const cmd = (input?.command as string) ?? (input?.cmd as string) ?? ''
    return {
      icon: faTerminal,
      label: 'bash',
      summary: cmd.split('\n')[0].trim().slice(0, 80),
      accent: '#f59e0b',
    }
  }

  // str_replace_editor (Claude-native file tool)
  if (n === 'str_replace_editor') {
    const cmd = (input?.command as string) ?? 'view'
    const path = (input?.path as string) ?? ''
    const shortPath = path.split('/').slice(-2).join('/')
    if (cmd === 'view')        return { icon: faEye,       label: 'view',   summary: shortPath, accent: 'var(--fg-dim)' }
    if (cmd === 'create')      return { icon: faFileLines, label: 'create', summary: shortPath, accent: 'var(--success)' }
    if (cmd === 'str_replace') return { icon: faPen,       label: 'edit',   summary: shortPath, accent: '#3b82f6' }
    if (cmd === 'insert')      return { icon: faPen,       label: 'insert', summary: shortPath, accent: '#3b82f6' }
    if (cmd === 'delete_file') return { icon: faFileLines, label: 'delete', summary: shortPath, accent: 'var(--error)' }
    return { icon: faPen, label: cmd, summary: shortPath, accent: '#3b82f6' }
  }

  // Edit tool
  if (n === 'edit') {
    const path = ((input?.file_path ?? input?.path) as string) ?? ''
    return { icon: faPen, label: 'edit', summary: path.split('/').slice(-2).join('/'), accent: '#3b82f6' }
  }

  // Write tool
  if (n === 'write') {
    const path = ((input?.file_path ?? input?.path) as string) ?? ''
    return { icon: faFileLines, label: 'write', summary: path.split('/').slice(-2).join('/'), accent: 'var(--success)' }
  }

  // Read tool
  if (n === 'read') {
    const path = ((input?.file_path ?? input?.path) as string) ?? ''
    return { icon: faEye, label: 'read', summary: path.split('/').slice(-2).join('/'), accent: 'var(--fg-dim)' }
  }

  // Grep
  if (n === 'grep') {
    const pattern = (input?.pattern as string) ?? ''
    return { icon: faMagnifyingGlass, label: 'grep', summary: pattern.slice(0, 50), accent: 'var(--fg-dim)' }
  }

  // Glob
  if (n === 'glob') {
    const pattern = (input?.pattern as string) ?? ''
    return { icon: faFolder, label: 'glob', summary: pattern, accent: 'var(--fg-dim)' }
  }

  // Search (any)
  if (n.includes('search')) {
    const q = ((input?.query ?? input?.q) as string) ?? ''
    return { icon: faMagnifyingGlass, label: 'search', summary: q.slice(0, 50), accent: 'var(--fg-dim)' }
  }

  // Fetch / HTTP
  if (n === 'web_fetch' || n === 'fetch' || n === 'http_request') {
    const url = (input?.url as string) ?? ''
    return { icon: faGlobe, label: 'fetch', summary: url.slice(0, 60), accent: 'var(--fg-dim)' }
  }

  return { icon: faWrench, label: name, summary: '', accent: 'var(--border)' }
}

// ─── Expanded content panels ──────────────────────────────────────────────────

function ThinkingContent({ text }: { text: string }) {
  return (
    <pre style={{ ...codeBlockStyle, color: 'var(--fg-muted)', maxHeight: 320, overflowY: 'auto', background: 'var(--bg-secondary)', border: 'none' }}>
      {text}
    </pre>
  )
}

function ToolContent({
  name,
  input,
  result,
  isError,
}: {
  name: string
  input?: Record<string, unknown> | null
  result?: string
  isError?: boolean
}) {
  const n = name.toLowerCase()

  // ── Bash
  if (n === 'bash' || n === 'execute_bash' || n === 'run_command') {
    const cmd = (input?.command ?? input?.cmd) as string | undefined
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        {cmd && (
          <div>
            <div style={labelStyle}>Command</div>
            <pre style={{ ...codeBlockStyle, background: '#1a1a2e', color: '#cdd6f4', borderColor: '#313244' }}>
              <span style={{ color: '#a6e3a1', userSelect: 'none' }}>$ </span>{cmd}
            </pre>
          </div>
        )}
        {result && (
          <div>
            <div style={{ ...labelStyle, color: isError ? 'var(--error)' : 'var(--fg-dim)' }}>
              {isError ? 'Error' : 'Output'}
            </div>
            <pre style={{ ...codeBlockStyle, color: isError ? 'var(--error)' : 'var(--fg)', maxHeight: 320, overflowY: 'auto' }}>
              {result}
            </pre>
          </div>
        )}
      </div>
    )
  }

  // ── str_replace_editor str_replace
  if (n === 'str_replace_editor' && input?.command === 'str_replace') {
    const path = input.path as string
    const oldStr = (input.old_str as string) ?? ''
    const newStr = (input.new_str as string) ?? ''
    const oldLines = oldStr ? oldStr.split('\n').length : 0
    const newLines = newStr ? newStr.split('\n').length : 0
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
        <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>
          {path} · <span style={{ color: 'var(--error)' }}>−{oldLines}</span>{' / '}
          <span style={{ color: 'var(--success)' }}>+{newLines}</span> lines
        </div>
        {oldStr && (
          <div>
            <div style={{ ...labelStyle, color: 'var(--error)' }}>Removed</div>
            <pre style={{ ...codeBlockStyle, color: 'var(--error)', background: 'color-mix(in srgb, var(--error) 6%, var(--surface))', borderColor: 'color-mix(in srgb, var(--error) 25%, transparent)', maxHeight: 200, overflowY: 'auto' }}>
              {oldStr.split('\n').map((l) => `- ${l}`).join('\n')}
            </pre>
          </div>
        )}
        {newStr && (
          <div>
            <div style={{ ...labelStyle, color: 'var(--success)' }}>Added</div>
            <pre style={{ ...codeBlockStyle, color: 'var(--success)', background: 'color-mix(in srgb, var(--success) 6%, var(--surface))', borderColor: 'color-mix(in srgb, var(--success) 25%, transparent)', maxHeight: 200, overflowY: 'auto' }}>
              {newStr.split('\n').map((l) => `+ ${l}`).join('\n')}
            </pre>
          </div>
        )}
        {result && <div style={{ fontSize: 11, color: isError ? 'var(--error)' : 'var(--fg-dim)', fontFamily: 'monospace' }}>{result}</div>}
      </div>
    )
  }

  // ── Edit tool (Pando native)
  if (n === 'edit') {
    const path = (input?.file_path ?? input?.path) as string | undefined
    const oldStr = input?.old_string as string | undefined
    const newStr = input?.new_string as string | undefined
    const oldLines = oldStr ? oldStr.split('\n').length : 0
    const newLines = newStr ? newStr.split('\n').length : 0
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
        {path && (
          <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>
            {path} · <span style={{ color: 'var(--error)' }}>−{oldLines}</span>{' / '}
            <span style={{ color: 'var(--success)' }}>+{newLines}</span> lines
          </div>
        )}
        {oldStr && (
          <div>
            <div style={{ ...labelStyle, color: 'var(--error)' }}>Removed</div>
            <pre style={{ ...codeBlockStyle, color: 'var(--error)', background: 'color-mix(in srgb, var(--error) 6%, var(--surface))', borderColor: 'color-mix(in srgb, var(--error) 25%, transparent)', maxHeight: 200, overflowY: 'auto' }}>
              {oldStr.split('\n').map((l) => `- ${l}`).join('\n')}
            </pre>
          </div>
        )}
        {newStr && (
          <div>
            <div style={{ ...labelStyle, color: 'var(--success)' }}>Added</div>
            <pre style={{ ...codeBlockStyle, color: 'var(--success)', background: 'color-mix(in srgb, var(--success) 6%, var(--surface))', borderColor: 'color-mix(in srgb, var(--success) 25%, transparent)', maxHeight: 200, overflowY: 'auto' }}>
              {newStr.split('\n').map((l) => `+ ${l}`).join('\n')}
            </pre>
          </div>
        )}
        {result && <div style={{ fontSize: 11, color: isError ? 'var(--error)' : 'var(--fg-dim)', fontFamily: 'monospace' }}>{result}</div>}
      </div>
    )
  }

  // ── str_replace_editor view / read
  if ((n === 'str_replace_editor' && input?.command === 'view') || n === 'read') {
    const path = (input?.path ?? input?.file_path) as string | undefined
    const truncated = result && result.length > 2000 ? result.slice(0, 2000) + '\n…(truncated)' : result
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
        {path && <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>{path}</div>}
        {truncated && (
          <pre style={{ ...codeBlockStyle, color: 'var(--fg-muted)', maxHeight: 320, overflowY: 'auto' }}>
            {truncated}
          </pre>
        )}
      </div>
    )
  }

  // ── str_replace_editor create / insert / write
  if (
    (n === 'str_replace_editor' && (input?.command === 'create' || input?.command === 'insert')) ||
    n === 'write'
  ) {
    const path = (input?.path ?? input?.file_path) as string | undefined
    const content = (input?.file_text ?? input?.new_str ?? input?.content) as string | undefined
    const truncated = content && content.length > 2000 ? content.slice(0, 2000) + '\n…(truncated)' : content
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
        {path && <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>{path}</div>}
        {truncated && (
          <pre style={{ ...codeBlockStyle, color: 'var(--success)', background: 'color-mix(in srgb, var(--success) 4%, var(--surface))', maxHeight: 320, overflowY: 'auto' }}>
            {truncated}
          </pre>
        )}
        {result && <div style={{ fontSize: 11, color: isError ? 'var(--error)' : 'var(--fg-dim)', fontFamily: 'monospace', marginTop: 2 }}>{result}</div>}
      </div>
    )
  }

  // ── Grep
  if (n === 'grep') {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          {typeof input?.pattern === 'string' && <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>pattern: {input.pattern}</div>}
          {typeof input?.path === 'string' && <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>path: {input.path}</div>}
          {typeof input?.glob === 'string' && <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)' }}>glob: {input.glob}</div>}
        </div>
        {result && (
          <pre style={{ ...codeBlockStyle, maxHeight: 320, overflowY: 'auto', color: 'var(--fg-muted)' }}>
            {result.length > 2000 ? result.slice(0, 2000) + '\n…(truncated)' : result}
          </pre>
        )}
      </div>
    )
  }

  // ── Generic fallback
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      {input && Object.keys(input).length > 0 && (
        <div>
          <div style={labelStyle}>Input</div>
          <pre style={{ ...codeBlockStyle, maxHeight: 200, overflowY: 'auto' }}>
            {JSON.stringify(input, null, 2)}
          </pre>
        </div>
      )}
      {result && (
        <div>
          <div style={{ ...labelStyle, color: isError ? 'var(--error)' : 'var(--fg-dim)' }}>
            {isError ? 'Error' : 'Output'}
          </div>
          <pre style={{ ...codeBlockStyle, color: isError ? 'var(--error)' : 'var(--fg)', maxHeight: 320, overflowY: 'auto' }}>
            {result}
          </pre>
        </div>
      )}
    </div>
  )
}

// ─── EventRow (exported for MessageList live state) ────────────────────────────

export interface EventRowProps {
  kind: 'thinking' | 'tool'
  // thinking
  thinking?: string
  // tool
  toolName?: string
  toolInput?: Record<string, unknown> | null
  toolResult?: string
  isError?: boolean
  isLive?: boolean
}

export function EventRow({ kind, thinking, toolName, toolInput, toolResult, isError, isLive }: EventRowProps) {
  const [expanded, setExpanded] = useState(false)

  const isDone = !isLive && (toolResult !== undefined || kind === 'thinking')

  // Metadata
  let icon: IconDefinition = faBrain
  let label = 'Thinking'
  let summary = ''
  let accent = 'var(--primary)'

  if (kind === 'tool' && toolName) {
    const meta = getToolMeta(toolName, toolInput)
    icon = meta.icon
    label = meta.label
    summary = meta.summary
    accent = isError ? 'var(--error)' : meta.accent
  }

  const statusColor = isError ? 'var(--error)' : isLive ? 'var(--fg-dim)' : isDone ? 'var(--success)' : 'var(--fg-dim)'

  return (
    <div
      style={{
        margin: '1px 1rem',
        borderLeft: `2px solid ${accent}`,
        borderRadius: '0 var(--radius-sm) var(--radius-sm) 0',
        overflow: 'hidden',
        fontSize: 12,
      }}
    >
      {/* Header row — always visible */}
      <button
        onClick={() => setExpanded((v) => !v)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          width: '100%',
          padding: '0.25rem 0.625rem',
          background: 'var(--surface)',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--fg-muted)',
          textAlign: 'left',
          minHeight: 26,
        }}
      >
        <FontAwesomeIcon icon={icon} style={{ fontSize: 10, color: accent, flexShrink: 0 }} />
        <span style={{ fontFamily: 'monospace', fontWeight: 600, fontSize: 11, color: 'var(--fg)', flexShrink: 0 }}>
          {label}
        </span>
        {summary && (
          <span style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--fg-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
            · {summary}
          </span>
        )}
        {!summary && <span style={{ flex: 1 }} />}

        {/* Status */}
        <span style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', flexShrink: 0 }}>
          {isLive && <LoadingSpinner size={9} />}
          {!isLive && isError && <span style={{ color: 'var(--error)', fontSize: 10 }}>✗</span>}
          {!isLive && !isError && isDone && <span style={{ color: 'var(--success)', fontSize: 10 }}>✓</span>}
        </span>

        <FontAwesomeIcon
          icon={expanded ? faChevronDown : faChevronRight}
          style={{ fontSize: 9, color: statusColor, flexShrink: 0 }}
        />
      </button>

      {/* Expanded content */}
      {expanded && (
        <div
          style={{
            padding: '0.5rem 0.625rem',
            background: 'var(--bg-secondary)',
            borderTop: `1px solid color-mix(in srgb, ${accent} 20%, var(--border))`,
          }}
        >
          {kind === 'thinking' ? (
            <ThinkingContent text={thinking ?? ''} />
          ) : (
            <ToolContent
              name={toolName ?? ''}
              input={toolInput}
              result={toolResult}
              isError={isError}
            />
          )}
        </div>
      )}
    </div>
  )
}

// ─── Markdown renderer ────────────────────────────────────────────────────────

function MarkdownContent({ text, streaming }: { text: string; streaming?: boolean }) {
  return (
    <div className="markdown-content">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
        {text}
      </ReactMarkdown>
      {streaming && (
        <span
          style={{
            display: 'inline-block',
            width: 2,
            height: '1em',
            background: 'var(--primary)',
            marginLeft: 2,
            verticalAlign: 'text-bottom',
            animation: 'blink 1s step-start infinite',
          }}
        />
      )}
    </div>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

interface MessageBubbleProps {
  message: Message
  streaming?: boolean
  streamingState?: StreamingState
}

export default function MessageBubble({ message, streaming, streamingState }: MessageBubbleProps) {
  const isUser = message.role === 'user'
  const timestamp = format(new Date(message.created_at), 'HH:mm')

  const textContent = message.content
    .filter((p) => p.type === 'text')
    .map((p) => p.text ?? '')
    .join('')

  const reasoningParts = message.content.filter((p) => p.type === 'reasoning')
  const toolParts = message.content.filter(
    (p) => p.type === 'tool_call' || p.type === 'tool_result',
  )

  // ── User message: simple bubble
  if (isUser) {
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'row-reverse',
          alignItems: 'flex-start',
          gap: '0.625rem',
          padding: '0.5rem 1rem',
        }}
      >
        <div
          style={{
            width: 32, height: 32, borderRadius: '50%',
            background: 'var(--primary)', flexShrink: 0,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: 'var(--primary-fg)',
          }}
        >
          <FontAwesomeIcon icon={faUser} style={{ fontSize: 13 }} />
        </div>
        <div
          style={{
            maxWidth: 'min(680px, 80%)',
            background: 'var(--primary)',
            color: 'var(--primary-fg)',
            borderRadius: 'var(--radius-md) var(--radius-sm) var(--radius-md) var(--radius-md)',
            padding: '0.625rem 0.875rem',
            fontSize: 14,
            lineHeight: 1.55,
            wordBreak: 'break-word',
          }}
        >
          <p style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{textContent}</p>
          <div style={{ fontSize: 10, marginTop: '0.375rem', color: 'rgba(255,255,255,0.65)', textAlign: 'left' }}>
            {timestamp}
          </div>
        </div>
      </div>
    )
  }

  // ── Assistant message: event rows + optional text bubble

  // Live streaming events (shown while streaming and no completed parts yet)
  const liveThinking = streaming && streamingState && streamingState.thinking.length > 0
  const liveTools = streaming && streamingState && streamingState.toolCalls.length > 0

  return (
    <div style={{ display: 'flex', flexDirection: 'column', padding: '0.375rem 0' }}>

      {/* Completed reasoning parts */}
      {!streaming && reasoningParts.map((p, i) => (
        <EventRow key={`r-${i}`} kind="thinking" thinking={p.text ?? ''} />
      ))}

      {/* Live thinking while streaming */}
      {liveThinking && (
        <EventRow kind="thinking" thinking={streamingState!.thinking} isLive />
      )}

      {/* Live tool calls while streaming */}
      {liveTools && streamingState!.toolCalls.map((tc) => (
        <EventRow
          key={tc.id}
          kind="tool"
          toolName={tc.name}
          toolInput={(() => { try { return JSON.parse(tc.input) } catch { return null } })()}
          toolResult={tc.result?.content}
          isError={tc.is_error}
          isLive={tc.result === undefined}
        />
      ))}

      {/* Completed tool calls */}
      {!streaming && toolParts.map((part: ContentPart, i) => (
        <EventRow
          key={`t-${i}`}
          kind="tool"
          toolName={part.tool_name ?? 'tool'}
          toolInput={part.tool_input ?? null}
          toolResult={part.tool_result}
          isError={part.is_error}
        />
      ))}

      {/* Text bubble — only if there is text content */}
      {(textContent || (streaming && !liveThinking && !liveTools)) && (
        <div
          style={{
            display: 'flex',
            alignItems: 'flex-start',
            gap: '0.625rem',
            padding: '0.25rem 1rem',
          }}
        >
          <div
            style={{
              width: 32, height: 32, borderRadius: '50%',
              background: 'var(--card-bg)', border: '1px solid var(--border)',
              flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: 'var(--fg-muted)',
            }}
          >
            <FontAwesomeIcon icon={faRobot} style={{ fontSize: 13 }} />
          </div>
          <div
            style={{
              maxWidth: 'min(680px, 80%)',
              background: 'var(--card-bg)',
              color: 'var(--fg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm) var(--radius-md) var(--radius-md) var(--radius-md)',
              padding: '0.625rem 0.875rem',
              fontSize: 14,
              lineHeight: 1.55,
              wordBreak: 'break-word',
            }}
          >
            {textContent ? (
              <MarkdownContent text={textContent} streaming={streaming} />
            ) : (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <LoadingSpinner size={14} />
                <span style={{ fontSize: 13, color: 'var(--fg-muted)' }}>Thinking…</span>
              </div>
            )}
            <div style={{ fontSize: 10, marginTop: '0.375rem', color: 'var(--fg-dim)', textAlign: 'right' }}>
              {timestamp}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
