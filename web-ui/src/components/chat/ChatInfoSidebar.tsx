import { useEffect, useMemo } from 'react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import clsx from 'clsx'
import { Badge, IconButton } from '@/components/ui'
import {
  Bot, Code, FileCode, FolderOpen, Gauge, Info, ListChecks, PanelRight, PanelRightClose, Shield, type LucideIcon,
} from '@/components/ui/icons'
import { PlanStatusIcon } from './PlanView'
import type { PlanEntry } from '@pando/client/hooks/useChat'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useFileChangesStore } from '@pando/client/stores/fileChangesStore'
import { useLSPStore } from '@pando/client/stores/lspStore'
import { useOrchestratorStore } from '@pando/client/stores/orchestratorStore'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useSandboxStore } from '@pando/client/stores/settingsStore'
import { useVersionStore } from '@pando/client/stores/versionStore'
import ExtensionSlot from '@/components/extensions/ExtensionSlot'

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
  const workspace = useProjectStore((s) => s.workspace)
  const fetchWorkspace = useProjectStore((s) => s.fetchWorkspace)
  const sandboxStatus = useSandboxStore((s) => s.info?.status)
  const refreshSandboxStatus = useSandboxStore((s) => s.refreshSandboxStatus)
  const versionStatus = useVersionStore((s) => s.status)
  const fetchVersion = useVersionStore((s) => s.fetchVersion)

  // Pando version + update availability. The server caches the GitHub lookup,
  // so fetching once per page load is enough.
  useEffect(() => {
    if (!infoSidebarOpen) return
    void fetchVersion()
  }, [infoSidebarOpen, fetchVersion])

  // Sandbox badge: refreshed on every open, so a settings change shows up.
  useEffect(() => {
    if (!infoSidebarOpen) return
    void refreshSandboxStatus()
  }, [infoSidebarOpen, refreshSandboxStatus])

  useEffect(() => {
    if (!infoSidebarOpen) return
    void fetchLSP()
  }, [infoSidebarOpen, fetchLSP])

  // Working directory of the connected instance. Fetched once per open (it only
  // changes when pando restarts) and kept if already known.
  useEffect(() => {
    if (!infoSidebarOpen || workspace) return
    void fetchWorkspace()
  }, [infoSidebarOpen, workspace, fetchWorkspace])

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
      <div className="chat-float chat-float--right">
        <IconButton
          size="sm"
          aria-label={t('chat.info.show')}
          tooltip
          icon={<PanelRight size={16} />}
          onClick={toggleInfoSidebar}
        />
      </div>
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
      <div className="chat-info-backdrop" onClick={toggleInfoSidebar} />
      <aside className="chat-info">
        {/* Header — session title + collapse control */}
        <div className="chat-info-head">
          <div className="chat-info-titles">
            <div className="chat-info-eyebrow">{t('chat.info.session')}</div>
            <div className="chat-info-title" title={session?.title}>
              {session?.title || t('chat.info.noSession')}
            </div>
          </div>
          <IconButton
            size="sm"
            aria-label={t('chat.info.hide')}
            tooltip
            icon={<PanelRightClose size={16} />}
            onClick={toggleInfoSidebar}
          />
        </div>

        <div className="chat-info-scroll">
          {/* Version — the running Pando build and whether `pando update` has a newer one */}
          {versionStatus?.version && (
            <Section icon={Info} title={t('chat.info.version')}>
              <div className="chat-info-mono">Pando {versionStatus.version}</div>
              {versionStatus.update_available && versionStatus.latest && (
                <div className="chat-info-note chat-info-update">
                  {t('version.updateAvailable', { latest: versionStatus.latest })}{' '}
                  <code>{versionStatus.update_command || 'pando update'}</code>
                </div>
              )}
            </Section>
          )}

          {/* Workspace — the instance's working directory, mirroring the TUI's "cwd:" line */}
          {workspace?.cwd && (
            <Section icon={FolderOpen} title={t('chat.info.workingDir')}>
              <div className="chat-info-mono" title={workspace.cwd}>{workspace.cwd}</div>
            </Section>
          )}

          {/* Sandbox — host command sandbox status, mirroring the TUI "Sandbox:" line */}
          {sandboxStatus && (
            <Section icon={Shield} title={t('chat.info.sandbox')}>
              <div className="chat-info-mono chat-sandbox" title={sandboxStatus.label}>
                <span
                  className={clsx(
                    'chat-sandbox-dot',
                    sandboxStatus.active ? 'chat-sandbox--ok' : sandboxStatus.enabled ? 'chat-sandbox--warn' : 'chat-sandbox--off',
                  )}
                  aria-hidden="true"
                />
                <span>{sandboxStatus.label}</span>
              </div>
              {sandboxStatus.enabled && !sandboxStatus.active && (
                <div className="chat-info-note">{t('chat.info.sandboxNotEnforced')}</div>
              )}
            </Section>
          )}

          {/* Usage */}
          {session && (
            <Section icon={Gauge} title={t('chat.info.usage')}>
              {contextWindow > 0 ? (
                <div className="chat-info-meter">
                  <div className="chat-info-row">
                    <span className="chat-info-label">{t('chat.info.context')}</span>
                    <span className="chat-info-value">
                      {formatCount(totalTokens)} / {formatCount(contextWindow)}
                      <span className="chat-info-label"> ({pct.toFixed(0)}%)</span>
                    </span>
                  </div>
                  <div className="chat-info-track">
                    <div
                      className={clsx('chat-info-fill', pct >= 90 ? 'chat-info-fill--danger' : pct >= 70 && 'chat-info-fill--warn')}
                      style={{ width: `${pct}%` }}
                    />
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
                <Row label={t('chat.info.cache')} value={`${formatCount(cacheRead)} / ${formatCount(cacheWrite)}`} />
              )}
              {reasoning > 0 && <Row label={t('chat.info.reasoning')} value={formatCount(reasoning)} />}
              {cost > 0 && <Row label={t('chat.info.cost')} value={`$${cost.toFixed(4)}`} accent />}
            </Section>
          )}

          {/* Subagents — only while some delegated task is unfinished */}
          {unfinished > 0 && (
            <Section icon={Bot} title={t('chat.info.subagents')}>
              <Row label={t('chat.info.runningPending')} value={`${running} / ${unfinished}`} accent />
            </Section>
          )}

          {/* Plan */}
          {plan.length > 0 && (
            <Section
              icon={ListChecks}
              title={t('chat.info.plan')}
              badge={`${plan.filter((e) => e.status === 'completed').length}/${plan.length}`}
            >
              {plan.map((entry, i) => (
                <div key={i} className={`chat-info-plan chat-info-plan--${entry.status}`}>
                  <PlanStatusIcon status={entry.status} size={13} />
                  <span title={entry.title}>{entry.title}</span>
                </div>
              ))}
            </Section>
          )}

          {/* LSPs */}
          {activeLSPs.length > 0 && (
            <Section icon={Code} title={t('chat.info.lsps')} badge={String(activeLSPs.length)}>
              <div className="chat-info-chips">
                {activeLSPs.map((language) => (
                  <Badge key={language}>{language}</Badge>
                ))}
              </div>
            </Section>
          )}

          {/* Modified files */}
          <Section icon={FileCode} title={t('chat.info.modifiedFiles')} badge={files.length ? String(files.length) : undefined}>
            {files.length === 0 ? (
              <div className="chat-info-note">{t('chat.info.noModifiedFiles')}</div>
            ) : (
              files.map((file) => (
                <div key={file.filePath} className="chat-info-file" title={file.filePath}>
                  <span className="chat-info-file-path">{`\u200e${file.filePath}\u200e`}</span>
                  <span className="chat-info-file-stats">
                    {file.additions > 0 && <span className="chat-add">+{file.additions}</span>}
                    {file.removals > 0 && <span className="chat-del">-{file.removals}</span>}
                  </span>
                </div>
              ))
            )}
          </Section>

          {/* Panels contributed by compiled-in extensions, always last so they
              cannot push core information out of view. */}
          <ExtensionSlot slot="chat-side" />
        </div>
      </aside>
    </>
  )
}

function Section({
  icon: Icon,
  title,
  badge,
  children,
}: {
  icon: LucideIcon
  title: string
  badge?: string
  children: ReactNode
}) {
  return (
    <section className="chat-info-section">
      <div className="chat-info-section-head">
        <Icon size={13} aria-hidden />
        <span className="chat-info-section-title">{title}</span>
        {badge && <span className="chat-info-count">{badge}</span>}
      </div>
      {children}
    </section>
  )
}

function Row({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="chat-info-row">
      <span className="chat-info-label">{label}</span>
      <span className={clsx('chat-info-value', accent && 'chat-info-value--accent')}>{value}</span>
    </div>
  )
}

/** formatCount renders a token count with K/M suffixes, mirroring the TUI sidebar. */
function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}
