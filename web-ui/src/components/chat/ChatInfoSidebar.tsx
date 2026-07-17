import { useEffect, useMemo } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faAngleDoubleLeft,
  faAngleDoubleRight,
  faCheck,
  faClock,
  faCode,
  faFileCode,
  faGaugeHigh,
  faListCheck,
  faRobot,
  faSpinner,
} from '@fortawesome/free-solid-svg-icons'
import type { PlanEntry } from '@/hooks/useChat'
import { useSessionStore } from '@/stores/sessionStore'
import { useFileChangesStore } from '@/stores/fileChangesStore'
import { useLSPStore } from '@/stores/lspStore'
import { useOrchestratorStore } from '@/stores/orchestratorStore'
import { useLayoutStore } from '@/stores/layoutStore'

/** Width of the expanded panel, and of the floating tab left behind when collapsed. */
const PANEL_WIDTH = 264
const RAIL_WIDTH = 34

/** Poll interval for the live subagent counts, in milliseconds. */
const SUBAGENT_POLL_MS = 5000

interface ChatInfoSidebarProps {
  /** Plan (TodoWrite) entries for the active session; empty hides the section. */
  plan?: PlanEntry[]
}

/**
 * ChatInfoSidebar is the web-UI counterpart of the TUI chat info sidebar
 * (internal/tui/components/chat/sidebar.go): it shows the session title, token
 * usage and cost, live subagent counts, configured LSPs, the current plan and
 * the files modified during the session. It is rendered to the right of both the
 * advanced and the simple chat views: expanded it takes its own width on desktop
 * and overlays the chat on mobile, collapsed it costs no width at all and leaves
 * only a floating tab over the chat's top-right corner.
 */
export default function ChatInfoSidebar({ plan = [] }: ChatInfoSidebarProps) {
  const { t } = useTranslation()
  const infoSidebarOpen = useLayoutStore((s) => s.infoSidebarOpen)
  const toggleInfoSidebar = useLayoutStore((s) => s.toggleInfoSidebar)

  const activeSessionId = useSessionStore((s) => s.activeSessionId)
  const sessions = useSessionStore((s) => s.sessions)
  const session = sessions.find((s) => s.id === activeSessionId)

  const changes = useFileChangesStore((s) => s.changes)
  const lspConfigs = useLSPStore((s) => s.configs)
  const fetchLSP = useLSPStore((s) => s.fetchLSP)
  const tasks = useOrchestratorStore((s) => s.tasks)
  const fetchTasks = useOrchestratorStore((s) => s.fetchTasks)

  useEffect(() => {
    if (!infoSidebarOpen) return
    void fetchLSP()
  }, [infoSidebarOpen, fetchLSP])

  // Keep the subagent counters live while the panel is visible.
  useEffect(() => {
    if (!infoSidebarOpen) return
    void fetchTasks()
    const id = window.setInterval(() => void fetchTasks(), SUBAGENT_POLL_MS)
    return () => window.clearInterval(id)
  }, [infoSidebarOpen, fetchTasks])

  const files = useMemo(
    () => Object.values(changes).sort((a, b) => a.filePath.localeCompare(b.filePath)),
    [changes],
  )

  const activeLSPs = useMemo(
    () => lspConfigs.filter((c) => !c.disabled).map((c) => c.language).sort(),
    [lspConfigs],
  )

  const running = tasks.filter((task) => task.status === 'running').length
  const unfinished = tasks.filter((task) => task.status === 'running' || task.status === 'pending').length

  if (!infoSidebarOpen) {
    return (
      <button
        onClick={toggleInfoSidebar}
        title={t('chat.info.show')}
        aria-label={t('chat.info.show')}
        style={railTabStyle}
      >
        <FontAwesomeIcon icon={faAngleDoubleLeft} style={{ fontSize: 12 }} />
      </button>
    )
  }

  const promptTokens = session?.prompt_tokens ?? 0
  const completionTokens = session?.completion_tokens ?? 0
  const totalTokens = promptTokens + completionTokens
  const contextWindow = session?.context_window ?? 0
  const cacheRead = session?.cache_read_tokens ?? 0
  const cacheWrite = session?.cache_creation_tokens ?? 0
  const reasoning = session?.reasoning_tokens ?? 0
  const cost = session?.cost ?? 0
  const pct = contextWindow > 0 ? Math.min((totalTokens / contextWindow) * 100, 100) : 0

  return (
    <>
      {/* Mobile backdrop — the panel turns into an overlay drawer below 768px. */}
      <div
        className="chat-info-backdrop"
        onClick={toggleInfoSidebar}
        style={{ display: 'none', position: 'absolute', inset: 0, zIndex: 99, background: 'rgba(0,0,0,0.4)' }}
      />
      <aside className="chat-info-panel" style={{ ...panelStyle, width: PANEL_WIDTH }}>
      {/* Header — session title + collapse control */}
      <div style={headerStyle}>
        <div style={{ flex: 1, overflow: 'hidden' }}>
          <div style={eyebrowStyle}>{t('chat.info.session')}</div>
          <div style={sessionTitleStyle} title={session?.title}>
            {session?.title || t('chat.info.noSession')}
          </div>
        </div>
        <button
          onClick={toggleInfoSidebar}
          title={t('chat.info.hide')}
          aria-label={t('chat.info.hide')}
          style={toggleButtonStyle}
        >
          <FontAwesomeIcon icon={faAngleDoubleRight} style={{ fontSize: 12 }} />
        </button>
      </div>

      <div style={scrollAreaStyle}>
        {/* Usage */}
        {session && (
          <Section icon={faGaugeHigh} title={t('chat.info.usage')}>
            {contextWindow > 0 ? (
              <div style={{ marginBottom: '0.6rem' }}>
                <div style={{ ...rowStyle, marginBottom: '0.3rem' }}>
                  <span style={labelStyle}>{t('chat.info.context')}</span>
                  <span style={valueStyle}>
                    {formatCount(totalTokens)} / {formatCount(contextWindow)}
                    <span style={{ color: 'var(--fg-dim)' }}> ({pct.toFixed(0)}%)</span>
                  </span>
                </div>
                <div style={meterTrackStyle}>
                  <div style={{ ...meterFillStyle, width: `${pct}%`, background: meterColor(pct) }} />
                </div>
              </div>
            ) : (
              <Row label={t('chat.info.totalTokens')} value={formatCount(totalTokens)} />
            )}

            <Row
              label={t('chat.info.inputOutput')}
              value={`${formatCount(promptTokens)} / ${formatCount(completionTokens)}`}
            />
            {(cacheRead > 0 || cacheWrite > 0) && (
              <Row
                label={t('chat.info.cache')}
                value={`${formatCount(cacheRead)} / ${formatCount(cacheWrite)}`}
              />
            )}
            {reasoning > 0 && <Row label={t('chat.info.reasoning')} value={formatCount(reasoning)} />}
            {cost > 0 && (
              <Row label={t('chat.info.cost')} value={`$${cost.toFixed(4)}`} accent />
            )}
          </Section>
        )}

        {/* Subagents — only while some delegated task is unfinished */}
        {unfinished > 0 && (
          <Section icon={faRobot} title={t('chat.info.subagents')}>
            <Row label={t('chat.info.runningPending')} value={`${running} / ${unfinished}`} accent />
          </Section>
        )}

        {/* Plan */}
        {plan.length > 0 && (
          <Section
            icon={faListCheck}
            title={t('chat.info.plan')}
            badge={`${plan.filter((e) => e.status === 'completed').length}/${plan.length}`}
          >
            {plan.map((entry, i) => (
              <div key={i} style={planItemStyle(entry.status)}>
                <span style={{ width: 12, flexShrink: 0, textAlign: 'center' }}>{planIcon(entry.status)}</span>
                <span
                  style={{
                    textDecoration: entry.status === 'completed' ? 'line-through' : 'none',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  title={entry.title}
                >
                  {entry.title}
                </span>
              </div>
            ))}
          </Section>
        )}

        {/* LSPs */}
        {activeLSPs.length > 0 && (
          <Section icon={faCode} title={t('chat.info.lsps')} badge={String(activeLSPs.length)}>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {activeLSPs.map((language) => (
                <span key={language} style={chipStyle}>
                  {language}
                </span>
              ))}
            </div>
          </Section>
        )}

        {/* Modified files */}
        <Section icon={faFileCode} title={t('chat.info.modifiedFiles')} badge={files.length ? String(files.length) : undefined}>
          {files.length === 0 ? (
            <div style={{ fontSize: 11, color: 'var(--fg-dim)' }}>{t('chat.info.noModifiedFiles')}</div>
          ) : (
            files.map((file) => (
              <div key={file.filePath} style={fileRowStyle} title={file.filePath}>
                <span style={filePathStyle}>{file.filePath}</span>
                <span style={{ display: 'flex', gap: 4, flexShrink: 0, fontSize: 10 }}>
                  {file.additions > 0 && <span style={{ color: 'var(--success)' }}>+{file.additions}</span>}
                  {file.removals > 0 && <span style={{ color: 'var(--error)' }}>-{file.removals}</span>}
                </span>
              </div>
            ))
          )}
        </Section>
      </div>

      <style>{`
        @media (max-width: 768px) {
          .chat-info-backdrop {
            display: block !important;
          }
          /* Overlay the chat body instead of stealing width from it. Both chat
             views mark the flex row that holds this panel as positioned. It
             stops short of the bottom so the message input stays reachable. */
          .chat-info-panel {
            position: absolute !important;
            top: 0 !important;
            right: 0 !important;
            bottom: auto !important;
            max-height: 65% !important;
            z-index: 100 !important;
            width: min(280px, 85vw) !important;
            border-bottom: 1px solid var(--border);
            border-bottom-left-radius: var(--radius-sm, 4px);
          }
          /* The backdrop must not swallow taps on the input either. */
          .chat-info-backdrop {
            bottom: auto !important;
            height: 65% !important;
          }
        }
      `}</style>
      </aside>
    </>
  )
}

function Section({
  icon,
  title,
  badge,
  children,
}: {
  icon: typeof faCode
  title: string
  badge?: string
  children: ReactNode
}) {
  return (
    <section style={sectionStyle}>
      <div style={sectionHeaderStyle}>
        <FontAwesomeIcon icon={icon} style={{ fontSize: 10, color: 'var(--primary)' }} />
        <span style={{ flex: 1 }}>{title}</span>
        {badge && <span style={badgeStyle}>{badge}</span>}
      </div>
      {children}
    </section>
  )
}

function Row({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div style={rowStyle}>
      <span style={labelStyle}>{label}</span>
      <span style={{ ...valueStyle, color: accent ? 'var(--primary)' : 'var(--fg)' }}>{value}</span>
    </div>
  )
}

/** formatCount renders a token count with K/M suffixes, mirroring the TUI sidebar. */
function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

/** meterColor shifts the context meter from primary to warning to error as it fills. */
function meterColor(pct: number): string {
  if (pct >= 90) return 'var(--error)'
  if (pct >= 70) return 'var(--warning, #f9e2af)'
  return 'var(--primary)'
}

function planIcon(status: string) {
  switch (status) {
    case 'completed':
      return <FontAwesomeIcon icon={faCheck} style={{ fontSize: 9, color: 'var(--success)' }} />
    case 'in_progress':
      return <FontAwesomeIcon icon={faSpinner} spin style={{ fontSize: 9, color: 'var(--primary)' }} />
    default:
      return <FontAwesomeIcon icon={faClock} style={{ fontSize: 9, color: 'var(--fg-dim)' }} />
  }
}

const panelStyle: CSSProperties = {
  flexShrink: 0,
  display: 'flex',
  flexDirection: 'column',
  background: 'var(--sidebar-bg, var(--bg-secondary))',
  borderLeft: '1px solid var(--border)',
  overflow: 'hidden',
}

/**
 * Collapsed state: a small tab floating over the top-right corner of the chat
 * instead of a rail in the flex flow, so the panel costs no chat width at all
 * while it is hidden.
 */
const railTabStyle: CSSProperties = {
  position: 'absolute',
  top: '0.5rem',
  right: 0,
  zIndex: 10,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: RAIL_WIDTH,
  height: 26,
  padding: 0,
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRight: 'none',
  borderRadius: 'var(--radius-sm) 0 0 var(--radius-sm)',
  boxShadow: '0 1px 4px rgba(0,0,0,0.15)',
  cursor: 'pointer',
  color: 'var(--fg-muted)',
}

const headerStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'flex-start',
  gap: '0.5rem',
  padding: '0.6rem 0.75rem',
  borderBottom: '1px solid var(--border)',
  flexShrink: 0,
}

const eyebrowStyle: CSSProperties = {
  fontSize: 9,
  fontWeight: 600,
  letterSpacing: '0.09em',
  textTransform: 'uppercase',
  color: 'var(--fg-dim)',
}

const sessionTitleStyle: CSSProperties = {
  fontSize: 12.5,
  fontWeight: 600,
  color: 'var(--fg)',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  marginTop: 2,
}

const toggleButtonStyle: CSSProperties = {
  background: 'none',
  border: 'none',
  cursor: 'pointer',
  color: 'var(--fg-muted)',
  padding: '0.15rem 0.25rem',
  height: 22,
  display: 'flex',
  alignItems: 'center',
}

const scrollAreaStyle: CSSProperties = {
  flex: 1,
  overflowY: 'auto',
  padding: '0.5rem',
  display: 'flex',
  flexDirection: 'column',
  gap: '0.5rem',
}

const sectionStyle: CSSProperties = {
  background: 'var(--surface, var(--bg))',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm, 4px)',
  padding: '0.5rem 0.6rem',
}

const sectionHeaderStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.4rem',
  marginBottom: '0.4rem',
  fontSize: 10,
  fontWeight: 700,
  letterSpacing: '0.08em',
  textTransform: 'uppercase',
  color: 'var(--fg-muted)',
}

const badgeStyle: CSSProperties = {
  fontSize: 9,
  fontWeight: 600,
  letterSpacing: 0,
  padding: '1px 5px',
  borderRadius: 999,
  background: 'color-mix(in srgb, var(--primary) 18%, transparent)',
  color: 'var(--primary)',
}

const rowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'baseline',
  justifyContent: 'space-between',
  gap: '0.5rem',
  fontSize: 11,
  lineHeight: 1.7,
}

const labelStyle: CSSProperties = {
  color: 'var(--fg-muted)',
  whiteSpace: 'nowrap',
}

const valueStyle: CSSProperties = {
  color: 'var(--fg)',
  fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
  fontSize: 11,
  whiteSpace: 'nowrap',
}

const meterTrackStyle: CSSProperties = {
  height: 4,
  borderRadius: 999,
  background: 'color-mix(in srgb, var(--fg-dim) 25%, transparent)',
  overflow: 'hidden',
}

const meterFillStyle: CSSProperties = {
  height: '100%',
  borderRadius: 999,
  transition: 'width 0.3s ease, background 0.3s ease',
}

const planItemStyle = (status: string): CSSProperties => ({
  display: 'flex',
  alignItems: 'center',
  gap: '0.4rem',
  fontSize: 11,
  lineHeight: 1.8,
  color: status === 'in_progress' ? 'var(--fg)' : 'var(--fg-muted)',
  fontWeight: status === 'in_progress' ? 600 : 400,
  opacity: status === 'completed' ? 0.65 : 1,
})

const chipStyle: CSSProperties = {
  fontSize: 10,
  padding: '2px 6px',
  borderRadius: 999,
  border: '1px solid var(--border)',
  background: 'var(--bg-secondary, var(--bg))',
  color: 'var(--fg-muted)',
}

const fileRowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: '0.4rem',
  fontSize: 11,
  lineHeight: 1.8,
}

const filePathStyle: CSSProperties = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  direction: 'rtl',
  textAlign: 'left',
  color: 'var(--fg)',
  fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
}
