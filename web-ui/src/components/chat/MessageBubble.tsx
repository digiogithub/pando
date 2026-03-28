import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faRobot, faUser, faWrench, faCheckCircle, faChevronDown, faChevronRight, faBrain, faTimesCircle } from '@fortawesome/free-solid-svg-icons'
import { format } from 'date-fns'
import 'highlight.js/styles/github-dark-dimmed.css'
import type { Message, ContentPart } from '@/types'
import type { StreamingState } from '@/hooks/useChat'
import { LiveToolCallBlock } from './MessageList'

// ─── Markdown renderer ───────────────────────────────────────────────────────

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

// ─── Tool call block ─────────────────────────────────────────────────────────

function ToolCallBlock({ part }: { part: ContentPart }) {
  const [expanded, setExpanded] = useState(false)
  const isResult = part.type === 'tool_result'

  return (
    <div
      style={{
        marginTop: '0.5rem',
        border: `1px solid ${isResult ? 'var(--success)' : 'var(--border)'}33`,
        borderRadius: 'var(--radius-sm)',
        overflow: 'hidden',
        fontSize: 12,
      }}
    >
      <button
        onClick={() => setExpanded((v) => !v)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          width: '100%',
          padding: '0.375rem 0.625rem',
          background: isResult ? 'color-mix(in srgb, var(--success) 8%, var(--surface))' : 'var(--surface)',
          border: 'none',
          cursor: 'pointer',
          color: isResult ? 'var(--success)' : 'var(--fg-muted)',
          textAlign: 'left',
        }}
      >
        <FontAwesomeIcon
          icon={isResult ? faCheckCircle : faWrench}
          style={{ fontSize: 10, flexShrink: 0 }}
        />
        <span style={{ fontFamily: 'monospace', fontWeight: 500 }}>
          {isResult ? 'tool_result' : (part.tool_name ?? 'tool_call')}
        </span>
        <FontAwesomeIcon
          icon={expanded ? faChevronDown : faChevronRight}
          style={{ fontSize: 9, marginLeft: 'auto', color: 'var(--fg-dim)' }}
        />
      </button>

      {expanded && (
        <div
          style={{
            padding: '0.5rem 0.625rem',
            background: 'var(--bg-secondary)',
            borderTop: `1px solid ${isResult ? 'var(--success)' : 'var(--border)'}33`,
          }}
        >
          {part.tool_input && (
            <div>
              <div style={{ color: 'var(--fg-muted)', marginBottom: 4, fontWeight: 600, fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Input</div>
              <pre
                style={{
                  margin: 0,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                  fontSize: 11,
                  color: 'var(--fg)',
                  background: 'var(--surface)',
                  padding: '0.375rem 0.5rem',
                  borderRadius: 'var(--radius-sm)',
                  border: '1px solid var(--border)',
                }}
              >
                {JSON.stringify(part.tool_input, null, 2)}
              </pre>
            </div>
          )}
          {part.tool_result && (
            <div style={{ marginTop: part.tool_input ? '0.5rem' : 0 }}>
              <div style={{ color: 'var(--fg-muted)', marginBottom: 4, fontWeight: 600, fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Output</div>
              <pre
                style={{
                  margin: 0,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                  fontSize: 11,
                  color: 'var(--fg)',
                  background: 'var(--surface)',
                  padding: '0.375rem 0.5rem',
                  borderRadius: 'var(--radius-sm)',
                  border: '1px solid var(--border)',
                  maxHeight: 300,
                  overflowY: 'auto',
                }}
              >
                {part.tool_result}
              </pre>
            </div>
          )}
        </div>
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

  const toolParts = message.content.filter(
    (p) => p.type === 'tool_call' || p.type === 'tool_result',
  )

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: isUser ? 'row-reverse' : 'row',
        alignItems: 'flex-start',
        gap: '0.625rem',
        padding: '0.5rem 1rem',
        maxWidth: '100%',
      }}
    >
      {/* Avatar */}
      <div
        style={{
          width: 32,
          height: 32,
          borderRadius: '50%',
          background: isUser ? 'var(--primary)' : 'var(--card-bg)',
          border: isUser ? 'none' : '1px solid var(--border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          flexShrink: 0,
          color: isUser ? 'var(--primary-fg)' : 'var(--fg-muted)',
        }}
      >
        <FontAwesomeIcon icon={isUser ? faUser : faRobot} style={{ fontSize: 13 }} />
      </div>

      {/* Bubble */}
      <div
        style={{
          maxWidth: 'min(680px, 80%)',
          background: isUser ? 'var(--primary)' : 'var(--card-bg)',
          color: isUser ? 'var(--primary-fg)' : 'var(--fg)',
          border: isUser ? 'none' : '1px solid var(--border)',
          borderRadius: isUser
            ? 'var(--radius-md) var(--radius-sm) var(--radius-md) var(--radius-md)'
            : 'var(--radius-sm) var(--radius-md) var(--radius-md) var(--radius-md)',
          padding: '0.625rem 0.875rem',
          fontSize: 14,
          lineHeight: 1.55,
          wordBreak: 'break-word',
        }}
      >
        {/* Thinking: live during streaming or collapsible when done */}
        {!isUser && streaming && streamingState && streamingState.thinking.length > 0 && (
          <div
            style={{
              border: '1px solid color-mix(in srgb, var(--primary) 30%, transparent)',
              borderRadius: 'var(--radius-sm)',
              overflow: 'hidden',
              fontSize: 12,
              marginBottom: '0.5rem',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.5rem',
                padding: '0.375rem 0.625rem',
                background: 'color-mix(in srgb, var(--primary) 8%, var(--surface))',
                color: 'var(--primary)',
              }}
            >
              <FontAwesomeIcon icon={faBrain} style={{ fontSize: 10 }} />
              <span style={{ fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                Thinking
              </span>
            </div>
            <div
              style={{
                padding: '0.5rem 0.625rem',
                background: 'var(--bg-secondary)',
                color: 'var(--fg-muted)',
                fontFamily: 'monospace',
                fontSize: 11,
                lineHeight: 1.55,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                maxHeight: 220,
                overflowY: 'auto',
              }}
            >
              {streamingState.thinking}
            </div>
          </div>
        )}

        {/* Live tool calls during streaming */}
        {!isUser && streaming && streamingState && streamingState.toolCalls.length > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem', marginBottom: '0.5rem' }}>
            {streamingState.toolCalls.map((tc) => (
              <LiveToolCallBlock key={tc.id} toolCall={tc} />
            ))}
          </div>
        )}

        {textContent && (
          isUser ? (
            <p style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{textContent}</p>
          ) : (
            <MarkdownContent text={textContent} streaming={streaming} />
          )
        )}

        {toolParts.map((part, i) => (
          <ToolCallBlock key={i} part={part} />
        ))}

        {/* Timestamp */}
        <div
          style={{
            fontSize: 10,
            marginTop: '0.375rem',
            color: isUser ? 'rgba(255,255,255,0.65)' : 'var(--fg-dim)',
            textAlign: isUser ? 'left' : 'right',
          }}
        >
          {timestamp}
        </div>
      </div>
    </div>
  )
}
