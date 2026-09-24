import { useRef, useState, useEffect, useCallback, type KeyboardEvent, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import clsx from 'clsx'
import SlashCommandMenu, { type SlashCommandItem } from './SlashCommandMenu'
import api from '@pando/client/services/api'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { ArrowUp, ChevronDown, Folder, Gauge, Square } from '@/components/ui/icons'

const MAX_CHARS = 8000
// Grows up to 9 lines (24px line-height, see .chat-composer-input), then scrolls.
const MAX_TEXTAREA_HEIGHT = 216

/** Human label for a model id (mirrors the status bar). */
function formatModel(id: string): string {
  if (!id) return ''
  if (id.startsWith('copilot.')) return 'Copilot ' + formatModel(id.slice(8))
  if (id.startsWith('claude-')) {
    const rest = id.slice(7)
    const dash = rest.indexOf('-')
    if (dash === -1) return 'Claude ' + rest.charAt(0).toUpperCase() + rest.slice(1)
    const name = rest.slice(0, dash)
    const version = rest.slice(dash + 1).replace(/-/g, '.')
    return 'Claude ' + name.charAt(0).toUpperCase() + name.slice(1) + ' ' + version
  }
  if (id.startsWith('gpt-')) return 'GPT-' + id.slice(4)
  if (id.startsWith('gemini-')) return 'Gemini ' + id.slice(7)
  const slash = id.lastIndexOf('/')
  return slash >= 0 ? id.slice(slash + 1) : id
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

interface ChatInputProps {
  onSend: (text: string) => void
  streaming: boolean
  onCancel: () => void
  disabled?: boolean
  goalActive?: boolean
}

export default function ChatInput({ onSend, streaming, onCancel, disabled, goalActive = false }: ChatInputProps) {
  const { t } = useTranslation()
  const [value, setValue] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Slash command menu state
  const [slashMenuOpen, setSlashMenuOpen] = useState(false)
  const [slashFilter, setSlashFilter] = useState('')
  const [slashSelectedIdx, setSlashSelectedIdx] = useState(0)
  const [slashCommands, setSlashCommands] = useState<SlashCommandItem[]>([])

  // Meta row + model chip data.
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const setModelSwitcherOpen = useLayoutStore((s) => s.setModelSwitcherOpen)
  const workspace = useProjectStore((s) => s.workspace)
  const fetchWorkspace = useProjectStore((s) => s.fetchWorkspace)
  const session = useSessionStore((s) => s.sessions.find((x) => x.id === s.activeSessionId))

  useEffect(() => {
    if (!workspace) void fetchWorkspace()
  }, [workspace, fetchWorkspace])

  // Fetch available commands on mount
  useEffect(() => {
    api.get<SlashCommandItem[]>('/api/v1/commands')
      .then((cmds) => setSlashCommands(Array.isArray(cmds) ? cmds : []))
      .catch(() => {})
  }, [])

  // Text pushed in from another surface — the Design Studio turning a preview
  // click into prompt context, or an empty-state suggestion. It is appended
  // rather than assigned so a half-written message is never destroyed.
  const pendingInsert = useChatDraftStore((s) => s.pendingInsert)
  useEffect(() => {
    if (pendingInsert === null) return
    const text = useChatDraftStore.getState().takeDraftInsert()
    if (!text) return
    setValue((current) => (current.trim() ? `${current.replace(/\s+$/, '')} ${text} ` : `${text} `))
    textareaRef.current?.focus()
  }, [pendingInsert])

  // Auto-resize: grow up to MAX_TEXTAREA_HEIGHT, then scroll
  const resize = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, MAX_TEXTAREA_HEIGHT)}px`
    // When at max height, keep scroll at bottom so latest line is visible
    if (el.scrollHeight > MAX_TEXTAREA_HEIGHT) el.scrollTop = el.scrollHeight
  }, [])

  useEffect(() => {
    resize()
  }, [value, resize])

  // Compute filtered commands for the menu
  const filteredCommands = slashCommands.filter((cmd) =>
    cmd.name.toLowerCase().startsWith(slashFilter.toLowerCase()),
  )

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setValue(newValue)

    // Detect slash command: value starts with "/" and is on a single line
    if (newValue.startsWith('/') && !newValue.includes('\n')) {
      setSlashFilter(newValue.slice(1))
      setSlashSelectedIdx(0)
      setSlashMenuOpen(true)
    } else {
      setSlashMenuOpen(false)
    }
  }

  const handleSlashSelect = (cmd: SlashCommandItem) => {
    setValue('/' + cmd.name + (cmd.acceptsArgs ? ' ' : ''))
    setSlashMenuOpen(false)
    textareaRef.current?.focus()
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Slash menu keyboard navigation
    if (slashMenuOpen && filteredCommands.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSlashSelectedIdx((prev) => Math.min(prev + 1, filteredCommands.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSlashSelectedIdx((prev) => Math.max(prev - 1, 0))
        return
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
        e.preventDefault()
        const selected = filteredCommands[slashSelectedIdx]
        if (selected) handleSlashSelect(selected)
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setSlashMenuOpen(false)
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleSend = () => {
    const text = value.trim()
    if (!text || disabled) return
    setValue('')
    setSlashMenuOpen(false)
    onSend(text)
  }

  const hasText = value.trim().length > 0
  const charCount = value.length
  const nearLimit = charCount > MAX_CHARS * 0.9

  const totalTokens = session ? session.prompt_tokens + session.completion_tokens : 0
  const contextWindow = session?.context_window ?? 0
  const pct = contextWindow > 0 ? Math.min((totalTokens / contextWindow) * 100, 100) : 0

  const hint = streaming
    ? t('chat.composer.hintRunning')
    : goalActive
      ? t('chat.composer.hintGoal')
      : t('chat.composer.hintIdle')

  return (
    <div className="chat-composer-wrap chat-column">
      <div className="chat-composer" onClick={(e) => { if (e.target === e.currentTarget) textareaRef.current?.focus() }}>
        <SlashCommandMenu
          commands={slashCommands}
          filter={slashFilter}
          selectedIndex={slashSelectedIdx}
          onSelect={handleSlashSelect}
          visible={slashMenuOpen}
        />

        <textarea
          ref={textareaRef}
          className="chat-composer-input"
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          placeholder={streaming ? t('chat.composer.placeholderRunning') : t('chat.composer.placeholder')}
          aria-label={t('chat.composer.placeholder')}
          rows={1}
          maxLength={MAX_CHARS}
          disabled={disabled}
        />

        <div className="chat-composer-actions">
          <span className="chat-composer-hint">{hint}</span>
          <span className="chat-spacer" />
          {defaultModel && (
            <button
              type="button"
              className="chat-chip"
              onClick={() => setModelSwitcherOpen(true)}
              title={t('common.clickToSwitchModel')}
            >
              <span className="chat-chip-label">{formatModel(defaultModel)}</span>
              <ChevronDown size={14} />
            </button>
          )}
          {/* While streaming, Send still queues feedback (steering) alongside Stop */}
          {streaming && (
            <button
              type="button"
              className="chat-send chat-send--stop"
              onClick={onCancel}
              title={t('chat.composer.stop')}
              aria-label={t('chat.composer.stop')}
            >
              <Square size={12} />
            </button>
          )}
          {(!streaming || hasText) && (
            <button
              type="button"
              className="chat-send"
              onClick={handleSend}
              disabled={!hasText || disabled}
              title={streaming ? t('chat.composer.queue') : t('chat.composer.send')}
              aria-label={streaming ? t('chat.composer.queue') : t('chat.composer.send')}
            >
              <ArrowUp size={18} strokeWidth={2} />
            </button>
          )}
        </div>
      </div>

      <div className="chat-composer-meta">
        {workspace?.cwd && (
          <span className="chat-meta-item chat-meta-item--path" title={workspace.cwd}>
            <Folder size={13} />
            <span>{`\u200e${workspace.cwd}\u200e`}</span>
          </span>
        )}
        <span className="chat-meta-spacer" />
        {nearLimit && (
          <span className="chat-meta-item chat-meta-item--warn">
            {t('chat.composer.chars', { count: charCount, max: MAX_CHARS.toLocaleString() })}
          </span>
        )}
        {session && totalTokens > 0 && (
          <span
            className="chat-meta-item"
            title={contextWindow > 0 ? `${formatCount(totalTokens)} / ${formatCount(contextWindow)}` : undefined}
          >
            <Gauge size={13} />
            {contextWindow > 0 ? (
              <>
                <span className={clsx('chat-meter', pct >= 90 ? 'chat-meter--danger' : pct >= 70 && 'chat-meter--warn')}>
                  <span style={{ width: `${pct}%` }} />
                </span>
                {t('chat.composer.context', { pct: pct.toFixed(0) })}
              </>
            ) : (
              t('chat.composer.tokens', { count: totalTokens, formatted: formatCount(totalTokens) })
            )}
          </span>
        )}
      </div>
    </div>
  )
}
