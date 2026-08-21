import { useEffect } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCloudArrowUp, faTriangleExclamation } from '@fortawesome/free-solid-svg-icons'
import { useTranslation } from 'react-i18next'
import { useExtensionMemoryStore } from '@pando/client/stores/extensionMemoryStore'

/**
 * Always-visible indicator for the memory capability.
 *
 * The capability ships memories and knowledge-base documents — project
 * content — to a store outside this machine, and the rule for that is that the
 * user can see it happening at all times and can see where it goes. This
 * component is that rule.
 *
 * It renders nothing when nothing is being shipped, which is the standard
 * build and every build with the gate closed. It never hides itself because a
 * request failed: the store keeps the last known status precisely so a failed
 * poll cannot read as "sync stopped".
 */
const REFRESH_MS = 30_000

export default function MemorySyncIndicator() {
  const { t } = useTranslation()
  const status = useExtensionMemoryStore((s) => s.status)
  const load = useExtensionMemoryStore((s) => s.load)
  const refresh = useExtensionMemoryStore((s) => s.refresh)

  useEffect(() => {
    void load()
    const timer = setInterval(() => { void refresh() }, REFRESH_MS)
    return () => clearInterval(timer)
  }, [load, refresh])

  if (!status?.active) return null

  const destinations = status.sinks
    .map((s) => s.destination || s.name || s.id)
    .filter(Boolean)
  const failing = status.sinks.some((s) => s.lastError)
  const pending = status.sinks.reduce((n, s) => n + s.pending, 0) + status.host.queued
  const sent = status.sinks.reduce((n, s) => n + s.sent, 0)

  // A sink that ships without reporting its own state is called out rather
  // than shown as idle: "shipping, state unknown" is the honest reading.
  const silent = status.sinks.some((s) => !s.reports)

  const label = status.dryRun
    ? t('memorySync.dryRun', 'memory sync (dry run)')
    : t('memorySync.active', 'memory sync')

  const title = [
    t('memorySync.title', 'Memories and KB documents written here are sent outside this machine.'),
    destinations.length > 0
      ? t('memorySync.destination', 'Destination: {{list}}', { list: destinations.join(', ') })
      : '',
    status.scopes?.length
      ? t('memorySync.scopes', 'Scopes: {{list}}', { list: status.scopes.join(', ') })
      : '',
    t('memorySync.counters', 'Sent: {{sent}} · Pending: {{pending}}', { sent, pending }),
    status.dryRun ? t('memorySync.dryRunHint', 'Dry run: nothing is actually sent.') : '',
    silent ? t('memorySync.silentSink', 'A sink reports no state of its own.') : '',
    failing ? t('memorySync.failing', 'Last error: {{error}}', {
      error: status.sinks.find((s) => s.lastError)?.lastError ?? '',
    }) : '',
  ].filter(Boolean).join('\n')

  const color = failing
    ? 'var(--error)'
    : status.dryRun
      ? 'var(--fg-muted)'
      : 'var(--warning, #d97706)'

  return (
    <span
      title={title}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.375rem',
        color,
        fontWeight: status.dryRun ? 400 : 600,
      }}
    >
      <FontAwesomeIcon
        icon={failing ? faTriangleExclamation : faCloudArrowUp}
        style={{ fontSize: 10 }}
      />
      <span>{label}</span>
      {pending > 0 && <span style={{ opacity: 0.8 }}>({pending})</span>}
    </span>
  )
}
