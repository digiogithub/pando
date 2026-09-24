import { useState, useRef, isValidElement, type ReactNode, type ReactElement } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import clsx from 'clsx'
import { format } from 'date-fns'
import type { Message, ContentPart, ToolCallStatus, ToolKind, ToolCallLocation } from '@pando/client/types'
import type { StreamingState, ActiveToolCall } from '@pando/client/hooks/useChat'
import MarkdownLink from '@/components/shared/MarkdownLink'
import { IconButton, Spinner } from '@/components/ui'
import {
  Brain, Check, ChevronRight, Copy, Eye, FilePen, FilePlus, FileText, Folder, Globe,
  Pencil, Search, SquareTerminal, Trash2, VenetianMask, Wrench, type LucideIcon,
} from '@/components/ui/icons'
import { copyToClipboard } from '@/utils/clipboard'

// ─── Tool metadata detection ──────────────────────────────────────────────────

/** Coarse category used to build the "Ran 3 commands · read 2 files" summary. */
type ActivityCategory = 'thought' | 'commands' | 'read' | 'edited' | 'searched' | 'fetched' | 'tools'

interface ToolMeta {
  icon: LucideIcon
  label: string
  summary: string
  category: ActivityCategory
}

const shortPath = (path: string) => path.split('/').slice(-2).join('/')

function getToolMeta(name: string, input?: Record<string, unknown> | null): ToolMeta {
  const n = name.toLowerCase()

  // bash / terminal
  if (n === 'bash' || n === 'execute_bash' || n === 'run_command') {
    const cmd = (input?.command as string) ?? (input?.cmd as string) ?? ''
    return { icon: SquareTerminal, label: 'bash', summary: cmd.split('\n')[0].trim().slice(0, 80), category: 'commands' }
  }

  // str_replace_editor (Claude-native file tool)
  if (n === 'str_replace_editor') {
    const cmd = (input?.command as string) ?? 'view'
    const path = shortPath((input?.path as string) ?? '')
    if (cmd === 'view')        return { icon: Eye,      label: 'view',   summary: path, category: 'read' }
    if (cmd === 'create')      return { icon: FilePlus, label: 'create', summary: path, category: 'edited' }
    if (cmd === 'str_replace') return { icon: Pencil,   label: 'edit',   summary: path, category: 'edited' }
    if (cmd === 'insert')      return { icon: Pencil,   label: 'insert', summary: path, category: 'edited' }
    if (cmd === 'delete_file') return { icon: Trash2,   label: 'delete', summary: path, category: 'edited' }
    return { icon: Pencil, label: cmd, summary: path, category: 'edited' }
  }

  if (n === 'edit') {
    return { icon: Pencil, label: 'edit', summary: shortPath(((input?.file_path ?? input?.path) as string) ?? ''), category: 'edited' }
  }
  if (n === 'write') {
    return { icon: FilePen, label: 'write', summary: shortPath(((input?.file_path ?? input?.path) as string) ?? ''), category: 'edited' }
  }
  if (n === 'read') {
    return { icon: FileText, label: 'read', summary: shortPath(((input?.file_path ?? input?.path) as string) ?? ''), category: 'read' }
  }
  if (n === 'grep') {
    return { icon: Search, label: 'grep', summary: ((input?.pattern as string) ?? '').slice(0, 50), category: 'searched' }
  }
  if (n === 'glob') {
    return { icon: Folder, label: 'glob', summary: (input?.pattern as string) ?? '', category: 'searched' }
  }
  if (n.includes('search')) {
    const q = ((input?.query ?? input?.q) as string) ?? ''
    return { icon: Search, label: 'search', summary: q.slice(0, 50), category: 'searched' }
  }
  if (n === 'web_fetch' || n === 'fetch' || n === 'http_request') {
    return { icon: Globe, label: 'fetch', summary: ((input?.url as string) ?? '').slice(0, 60), category: 'fetched' }
  }

  return { icon: Wrench, label: name, summary: '', category: 'tools' }
}

function kindToLabel(kind: ToolKind): string {
  switch (kind) {
    case 'execute': return 'bash'
    case 'edit': return 'edit'
    case 'read': return 'read'
    case 'search': return 'search'
    case 'fetch': return 'fetch'
    case 'think': return 'agent'
    case 'switch_mode': return 'mode'
    default: return kind
  }
}

function kindToCategory(kind: ToolKind | undefined, fallback: ActivityCategory): ActivityCategory {
  switch (kind) {
    case 'execute': return 'commands'
    case 'edit': return 'edited'
    case 'read': return 'read'
    case 'search': return 'searched'
    case 'fetch': return 'fetched'
    default: return fallback
  }
}

const truncate = (text: string, max = 2000) => (text.length > max ? text.slice(0, max) + '\n…(truncated)' : text)

// ─── Copy helper ──────────────────────────────────────────────────────────────

function CopyIconButton({ text, label }: { text: string | (() => string); label: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    const value = typeof text === 'function' ? text() : text
    if (!value) return
    if (await copyToClipboard(value)) {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <IconButton
      size="sm"
      aria-label={copied ? t('chat.copied') : label}
      tooltip
      icon={copied ? <Check size={14} /> : <Copy size={14} />}
      onClick={() => void handleCopy()}
    />
  )
}

// ─── Expanded content panels ──────────────────────────────────────────────────

function ToolBlock({ label, tone, children }: { label?: string; tone?: 'error' | 'removed' | 'added'; children: ReactNode }) {
  return (
    <div className="chat-tool-block">
      {label && <div className={clsx('chat-tool-label', tone && `chat-tool-label--${tone}`)}>{label}</div>}
      {children}
    </div>
  )
}

/** Old/new strings of an edit, shown as removed / added blocks. */
function EditDiff({ path, oldStr, newStr, result, isError }: {
  path?: string; oldStr: string; newStr: string; result?: string; isError?: boolean
}) {
  const { t } = useTranslation()
  const oldLines = oldStr ? oldStr.split('\n').length : 0
  const newLines = newStr ? newStr.split('\n').length : 0
  return (
    <>
      {path && (
        <div className="chat-tool-meta">
          {path}
          {(oldStr || newStr) && (
            <> · <span className="chat-del">−{oldLines}</span>{' / '}<span className="chat-add">+{newLines}</span> {t('chat.tool.lines')}</>
          )}
        </div>
      )}
      {oldStr && (
        <ToolBlock label={t('chat.tool.removed')} tone="removed">
          <pre className="chat-tool-pre chat-tool-pre--removed">{oldStr.split('\n').map((l) => `- ${l}`).join('\n')}</pre>
        </ToolBlock>
      )}
      {newStr && (
        <ToolBlock label={t('chat.tool.added')} tone="added">
          <pre className="chat-tool-pre chat-tool-pre--added">{newStr.split('\n').map((l) => `+ ${l}`).join('\n')}</pre>
        </ToolBlock>
      )}
      {result && <div className={clsx('chat-tool-meta', isError ? 'chat-tool-meta--error' : 'chat-tool-meta--faint')}>{result}</div>}
    </>
  )
}

function ToolContent({
  name, input, result, isError, diff, terminal, images,
}: {
  name: string
  input?: Record<string, unknown> | null
  result?: string
  isError?: boolean
  diff?: { file_path: string; old_string?: string; new_string?: string; new_content?: string }
  terminal?: { terminal_id: string; exit_code: number }
  images?: string[]
}) {
  const { t } = useTranslation()
  const n = name.toLowerCase()

  // Image tool results (e.g. screenshots): render thumbnails, never base64 text.
  if (images && images.length > 0) {
    return (
      <>
        {result && <div className="chat-tool-meta chat-tool-meta--faint">{result}</div>}
        <div className="chat-tool-images">
          {images.map((src, i) => <img key={i} src={src} alt={`tool image ${i + 1}`} />)}
        </div>
      </>
    )
  }

  const output = result && (
    <ToolBlock label={isError ? t('chat.tool.error') : t('chat.tool.output')} tone={isError ? 'error' : undefined}>
      <pre className={clsx('chat-tool-pre', isError && 'chat-tool-pre--error')}>{result}</pre>
    </ToolBlock>
  )

  // Bash
  if (n === 'bash' || n === 'execute_bash' || n === 'run_command') {
    const cmd = (input?.command ?? input?.cmd) as string | undefined
    return (
      <>
        {cmd && (
          <ToolBlock label={t('chat.tool.command')}>
            <pre className="chat-tool-pre"><span className="chat-tool-prompt">$ </span>{cmd}</pre>
          </ToolBlock>
        )}
        {output}
      </>
    )
  }

  // str_replace_editor str_replace
  if (n === 'str_replace_editor' && input?.command === 'str_replace') {
    return <EditDiff path={input.path as string} oldStr={(input.old_str as string) ?? ''} newStr={(input.new_str as string) ?? ''} result={result} isError={isError} />
  }

  // Edit tool (Pando native)
  if (n === 'edit') {
    return (
      <EditDiff
        path={(input?.file_path ?? input?.path) as string | undefined}
        oldStr={(input?.old_string as string) ?? ''}
        newStr={(input?.new_string as string) ?? ''}
        result={result}
        isError={isError}
      />
    )
  }

  // str_replace_editor view / read
  if ((n === 'str_replace_editor' && input?.command === 'view') || n === 'read') {
    const path = (input?.path ?? input?.file_path) as string | undefined
    return (
      <>
        {path && <div className="chat-tool-meta">{path}</div>}
        {result && <pre className="chat-tool-pre chat-tool-pre--muted">{truncate(result)}</pre>}
      </>
    )
  }

  // str_replace_editor create / insert / write
  if ((n === 'str_replace_editor' && (input?.command === 'create' || input?.command === 'insert')) || n === 'write') {
    const path = (input?.path ?? input?.file_path) as string | undefined
    const content = (input?.file_text ?? input?.new_str ?? input?.content) as string | undefined
    return (
      <>
        {path && <div className="chat-tool-meta">{path}</div>}
        {content && <pre className="chat-tool-pre chat-tool-pre--added">{truncate(content)}</pre>}
        {result && <div className={clsx('chat-tool-meta', isError ? 'chat-tool-meta--error' : 'chat-tool-meta--faint')}>{result}</div>}
      </>
    )
  }

  // Grep
  if (n === 'grep') {
    return (
      <>
        {typeof input?.pattern === 'string' && <div className="chat-tool-meta">pattern: {input.pattern}</div>}
        {typeof input?.path === 'string' && <div className="chat-tool-meta">path: {input.path}</div>}
        {typeof input?.glob === 'string' && <div className="chat-tool-meta">glob: {input.glob}</div>}
        {result && <pre className="chat-tool-pre chat-tool-pre--muted">{truncate(result)}</pre>}
      </>
    )
  }

  // Backend-provided diff (for edit/write tools when input wasn't parsed client-side)
  if (diff && diff.file_path) {
    return (
      <EditDiff
        path={diff.file_path}
        oldStr={diff.old_string ?? ''}
        newStr={diff.new_string ?? diff.new_content ?? ''}
        result={result}
        isError={isError}
      />
    )
  }

  // Generic fallback
  return (
    <>
      {input && Object.keys(input).length > 0 && (
        <ToolBlock label={t('chat.tool.input')}>
          <pre className="chat-tool-pre chat-tool-pre--short">{JSON.stringify(input, null, 2)}</pre>
        </ToolBlock>
      )}
      {output}
      {terminal && <div className="chat-tool-meta chat-tool-meta--faint">{t('chat.tool.exitCode', { code: terminal.exit_code })}</div>}
    </>
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
  // Rich metadata from backend (mirrors ACP)
  backendTitle?: string
  backendKind?: ToolKind
  toolStatus?: ToolCallStatus
  locations?: ToolCallLocation[]
  diff?: { file_path: string; old_string?: string; new_string?: string; new_content?: string }
  terminal?: { terminal_id: string; exit_code: number }
  images?: string[]
}

interface ResolvedEvent {
  icon: LucideIcon
  label: string
  summary: string
  category: ActivityCategory
  status: ToolCallStatus
  failed: boolean
  live: boolean
}

function resolveEvent(p: EventRowProps, t: TFunction): ResolvedEvent {
  const status: ToolCallStatus = p.toolStatus
    ?? (p.isError ? 'failed' : p.isLive ? 'in_progress' : p.toolResult !== undefined ? 'completed' : 'pending')
  if (p.kind === 'tool' && p.toolName) {
    const meta = getToolMeta(p.toolName, p.toolInput)
    return {
      icon: meta.icon,
      // Backend-provided title/kind win over local detection.
      label: p.backendKind ? kindToLabel(p.backendKind) : meta.label,
      summary: p.backendTitle ?? meta.summary,
      category: kindToCategory(p.backendKind, meta.category),
      status,
      failed: Boolean(p.isError) || status === 'failed',
      live: status === 'in_progress' || (Boolean(p.isLive) && status !== 'completed' && status !== 'failed'),
    }
  }
  return {
    icon: Brain,
    label: p.isLive ? t('chat.thinking') : t('chat.thought'),
    summary: '',
    category: 'thought',
    status,
    failed: false,
    live: Boolean(p.isLive),
  }
}

function StatusMark({ ev }: { ev: ResolvedEvent }) {
  if (ev.live) return <Spinner size={12} />
  if (ev.failed) return <span className="chat-status-dot chat-status-dot--error" />
  if (ev.status === 'completed') return <span className="chat-status-dot chat-status-dot--done" />
  return <span className="chat-status-dot" />
}

export function EventRow(props: EventRowProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const ev = resolveEvent(props, t)
  const Icon = ev.icon
  const { kind, thinking, toolName, toolInput, toolResult, isError, locations, diff, terminal, images } = props

  return (
    <div className={clsx('chat-event', ev.failed && 'chat-event--error')}>
      <button type="button" className="chat-event-row" aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>
        <Icon size={14} className="chat-event-icon" />
        <span className="chat-event-name">{ev.label}</span>
        <span className="chat-event-arg">{ev.summary}</span>
        {(kind === 'tool' || ev.live) && (
          <span className="chat-event-status" title={ev.status}><StatusMark ev={ev} /></span>
        )}
        <ChevronRight size={14} className="chat-chevron" />
      </button>

      {expanded && (
        <div className="chat-event-detail">
          {locations && locations.length > 0 && (
            <div className="chat-locations">
              {locations.map((loc, i) => <span key={i} className="chat-location">{loc.path}</span>)}
            </div>
          )}
          {kind === 'thinking' ? (
            <pre className="chat-tool-pre chat-tool-pre--muted">{thinking ?? ''}</pre>
          ) : (
            <ToolContent
              name={toolName ?? ''}
              input={toolInput}
              result={toolResult}
              isError={isError}
              diff={diff}
              terminal={terminal}
              images={images}
            />
          )}
        </div>
      )}
    </div>
  )
}

// ─── Activity group: consecutive tool calls / thinking collapsed into one row ──

const CATEGORY_ORDER: ActivityCategory[] = ['commands', 'read', 'edited', 'searched', 'fetched', 'tools', 'thought']

function ActivityGroup({ events }: { events: { key: string; props: EventRowProps }[] }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)

  // A lone event needs no summary: its own row says more than "Ran 1 command".
  if (events.length === 1) {
    return <div className="chat-activity"><EventRow {...events[0].props} /></div>
  }

  const resolved = events.map((e) => resolveEvent(e.props, t))
  const counts = new Map<ActivityCategory, number>()
  for (const ev of resolved) counts.set(ev.category, (counts.get(ev.category) ?? 0) + 1)
  const summary = CATEGORY_ORDER
    .filter((c) => counts.has(c))
    .map((c) => t(`chat.activity.${c}`, { count: counts.get(c) }))
    .join(' · ')
  const live = [...resolved].reverse().find((ev) => ev.live)
  const failures = resolved.filter((ev) => ev.failed).length

  return (
    <div className="chat-activity">
      <button type="button" className="chat-activity-summary" aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>
        <ChevronRight size={14} className="chat-chevron" />
        <span className="chat-activity-text">{summary}</span>
        {failures > 0 && <span className="chat-activity-error">· {t('chat.activity.failed', { count: failures })}</span>}
        {live && (
          <>
            <Spinner size={12} />
            <span className="chat-activity-live">{[live.label, live.summary].filter(Boolean).join(' ')}</span>
          </>
        )}
      </button>
      {expanded && (
        <div className="chat-activity-list">
          {events.map((e) => <EventRow key={e.key} {...e.props} />)}
        </div>
      )}
    </div>
  )
}

// ─── Code block with header (language + copy) ─────────────────────────────────
// The copy button reads the <pre>'s live DOM text via a getter rather than a
// plain string, since the code block content can still be streaming.

function PreWithCopy({ children, ...props }: React.HTMLAttributes<HTMLPreElement>) {
  const { t } = useTranslation()
  const preRef = useRef<HTMLPreElement>(null)
  let lang = ''
  if (isValidElement(children)) {
    const cls = (children as ReactElement<{ className?: string }>).props.className ?? ''
    lang = /language-([\w+#.-]+)/.exec(cls)?.[1] ?? ''
  }
  return (
    <div className="chat-code">
      <div className="chat-code-header">
        <span className="chat-code-lang">{lang || 'text'}</span>
        <CopyIconButton text={() => preRef.current?.textContent ?? ''} label={t('chat.copyCode')} />
      </div>
      <pre ref={preRef} {...props}>{children}</pre>
    </div>
  )
}

// ─── Markdown renderer ────────────────────────────────────────────────────────

function MarkdownContent({ text }: { text: string }) {
  return (
    <div className="markdown-content">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{ pre: PreWithCopy, a: MarkdownLink }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

// ─── Streaming indicators ─────────────────────────────────────────────────────

export function ThinkingShimmer() {
  const { t } = useTranslation()
  return (
    <div className="chat-shimmer" role="status">
      <span className="chat-shimmer-text">{t('chat.thinking')}</span>
    </div>
  )
}

function StreamingDots() {
  return (
    <div className="chat-shimmer" aria-hidden>
      <span className="chat-dots"><span /><span /><span /></span>
    </div>
  )
}

// ─── Persona / System message (collapsible) ───────────────────────────────────

function PersonaRow({ text, timestamp }: { text: string; timestamp: string }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="chat-activity">
      <div className="chat-event">
        <button type="button" className="chat-event-row" aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>
          <VenetianMask size={14} className="chat-event-icon" />
          <span className="chat-event-name">{t('chat.persona')}</span>
          <span className="chat-event-arg">{timestamp}</span>
          <ChevronRight size={14} className="chat-chevron" />
        </button>
        {expanded && (
          <div className="chat-event-detail">
            <pre className="chat-tool-pre chat-tool-pre--muted">{text}</pre>
          </div>
        )}
      </div>
    </div>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

type Entry =
  | { type: 'text'; key: string; text: string }
  | { type: 'event'; key: string; props: EventRowProps }

type Block =
  | { type: 'text'; key: string; text: string }
  | { type: 'group'; key: string; events: { key: string; props: EventRowProps }[] }

/** Merge consecutive tool/thinking entries into activity groups. */
function toBlocks(entries: Entry[]): Block[] {
  const blocks: Block[] = []
  for (const e of entries) {
    if (e.type === 'text') {
      blocks.push(e)
      continue
    }
    const last = blocks[blocks.length - 1]
    if (last && last.type === 'group') last.events.push({ key: e.key, props: e.props })
    else blocks.push({ type: 'group', key: `g-${e.key}`, events: [{ key: e.key, props: e.props }] })
  }
  return blocks
}

function liveToolProps(tc: ActiveToolCall): EventRowProps {
  return {
    kind: 'tool',
    toolName: tc.name,
    toolInput: (() => { try { return JSON.parse(tc.input) } catch { return null } })(),
    toolResult: tc.result?.content,
    isError: tc.is_error,
    isLive: tc.status === 'pending' || tc.status === 'in_progress',
    backendTitle: tc.title,
    backendKind: tc.kind,
    toolStatus: tc.status,
    locations: tc.locations,
    diff: tc.diff,
    terminal: tc.terminal,
    images: tc.result?.images,
  }
}

/** Renderable entries of one assistant message, keys namespaced by message id. */
function assistantEntries(message: Message, streamingState?: StreamingState): Entry[] {
  const entries: Entry[] = []
  const id = message.id
  if (streamingState) {
    streamingState.items.forEach((item, i) => {
      if (item.type === 'thinking') entries.push({ type: 'event', key: `${id}-thinking-${i}`, props: { kind: 'thinking', thinking: item.text, isLive: true } })
      else if (item.type === 'text') entries.push({ type: 'text', key: `${id}-text-${i}`, text: item.text })
      else if (item.type === 'tool') {
        const tc = streamingState.toolCalls.find((x) => x.id === item.id)
        if (tc) entries.push({ type: 'event', key: `${id}-tool-${item.id}`, props: liveToolProps(tc) })
      }
    })
    return entries
  }
  message.content.forEach((part: ContentPart, i) => {
    if (part.type === 'reasoning') entries.push({ type: 'event', key: `${id}-r-${i}`, props: { kind: 'thinking', thinking: part.text ?? '' } })
    else if (part.type === 'tool_call') {
      entries.push({
        type: 'event',
        key: `${id}-t-${i}`,
        props: { kind: 'tool', toolName: part.tool_name ?? 'tool', toolInput: part.tool_input ?? null, toolResult: part.tool_result, isError: part.is_error },
      })
    } else if (part.type === 'text' && part.text) entries.push({ type: 'text', key: `${id}-txt-${i}`, text: part.text })
  })
  // Fallback for messages with no typed parts (legacy format)
  const textContent = messageText(message)
  if (message.content.every((p) => p.type !== 'text' && p.type !== 'tool_call' && p.type !== 'reasoning') && textContent) {
    entries.push({ type: 'text', key: `${id}-txt-legacy`, text: textContent })
  }
  return entries
}

const messageText = (message: Message) =>
  message.content.filter((p) => p.type === 'text').map((p) => p.text ?? '').join('')

interface AssistantTurnProps {
  /** Consecutive assistant messages rendered as one turn. */
  messages: Message[]
  /** True while the LAST message of the turn is streaming. */
  streaming?: boolean
  streamingState?: StreamingState
}

/**
 * One assistant turn: consecutive assistant messages (an agent loop stores a
 * message per step) rendered as a single block of prose, with every run of
 * tool calls / thinking between two texts collapsed into one activity row.
 */
export function AssistantTurn({ messages, streaming, streamingState }: AssistantTurnProps) {
  const { t } = useTranslation()
  const isStreaming = Boolean(streaming && streamingState)
  const last = messages[messages.length - 1]

  const entries: Entry[] = []
  messages.forEach((m, i) => {
    const live = isStreaming && i === messages.length - 1 ? streamingState : undefined
    entries.push(...assistantEntries(m, live))
  })

  const blocks = toBlocks(entries)
  const lastBlock = blocks[blocks.length - 1]
  const turnText = entries.filter((e) => e.type === 'text').map((e) => (e as { text: string }).text).join('\n\n')

  if (!isStreaming && blocks.length === 0) return null

  return (
    <div className="chat-msg chat-msg--assistant">
      {isStreaming && blocks.length === 0 && <ThinkingShimmer />}
      {blocks.map((b) =>
        b.type === 'text'
          ? <div key={b.key} className="chat-prose"><MarkdownContent text={b.text} /></div>
          : <ActivityGroup key={b.key} events={b.events} />,
      )}
      {isStreaming && lastBlock?.type === 'text' && <StreamingDots />}
      {!isStreaming && turnText && (
        <div className="chat-msg-actions">
          <CopyIconButton text={turnText} label={t('chat.copyMessage')} />
          <time dateTime={last.created_at}>{format(new Date(last.created_at), 'HH:mm')}</time>
        </div>
      )}
    </div>
  )
}

interface MessageBubbleProps {
  message: Message
  streaming?: boolean
  streamingState?: StreamingState
}

export default function MessageBubble({ message, streaming, streamingState }: MessageBubbleProps) {
  const { t } = useTranslation()
  const timestamp = format(new Date(message.created_at), 'HH:mm')
  const textContent = messageText(message)

  // System / Persona message: collapsible, hidden by default
  if (message.role === 'system') {
    return <PersonaRow text={textContent} timestamp={timestamp} />
  }

  if (message.role !== 'user') {
    return <AssistantTurn messages={[message]} streaming={streaming} streamingState={streamingState} />
  }

  // User message: right-aligned bubble, attachments above it
  const images = message.content.filter((p) => p.type === 'image' && p.image_url).map((p) => p.image_url as string)
  if (!textContent && images.length === 0) return null
  return (
    <div className="chat-msg chat-msg--user">
      {images.length > 0 && (
        <div className="chat-attachments">
          {images.map((src, i) => (
            <a key={i} href={src} target="_blank" rel="noreferrer">
              <img className="chat-attachment" src={src} alt={`attachment ${i + 1}`} />
            </a>
          ))}
        </div>
      )}
      {textContent && (
        <div className="chat-user-bubble">
          <MarkdownContent text={textContent} />
        </div>
      )}
      <div className="chat-msg-actions">
        <time dateTime={message.created_at}>{timestamp}</time>
        {textContent && <CopyIconButton text={textContent} label={t('chat.copyMessage')} />}
      </div>
    </div>
  )
}
