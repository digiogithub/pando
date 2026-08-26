import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faClockRotateLeft, faRotateLeft } from '@fortawesome/free-solid-svg-icons'
import { format } from 'date-fns'
import { useDesignStore } from '@pando/client/stores/designStore'

interface VersionTimelineProps {
  artifactId: string
  currentVersion: number
}

/**
 * VersionTimeline lists the artifact's accepted iterations, newest first, and
 * lets the user go back to one.
 *
 * A checkout is a directory-scoped snapshot revert on the server, so it can
 * never touch work outside the artifact — but it does rewrite the files in the
 * user's tree, which is why the button says so plainly instead of being a
 * one-click undo hidden in a hover state.
 */
export default function VersionTimeline({ artifactId, currentVersion }: VersionTimelineProps) {
  const { t } = useTranslation()
  const versions = useDesignStore((s) => s.versions)
  const checkout = useDesignStore((s) => s.checkout)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 0.75rem', borderBottom: '1px solid var(--border)' }}>
        <FontAwesomeIcon icon={faClockRotateLeft} style={{ fontSize: 11, color: 'var(--fg-muted)' }} />
        <span style={{ fontSize: 12, fontWeight: 600 }}>{t('design.versions.title')}</span>
      </div>

      <div style={{ flex: 1, overflow: 'auto' }}>
        {versions.length === 0 && (
          <div style={{ padding: '0.75rem', fontSize: 12, color: 'var(--fg-muted)' }}>{t('design.versions.empty')}</div>
        )}
        {versions.map((version) => {
          const isCurrent = version.number === currentVersion
          return (
            <div
              key={version.number}
              style={{
                padding: '0.5rem 0.75rem',
                borderBottom: '1px solid var(--border)',
                borderLeft: isCurrent ? '2px solid var(--primary)' : '2px solid transparent',
                background: isCurrent ? 'var(--surface)' : 'transparent',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                <span style={{ fontSize: 11, fontWeight: 700 }}>v{version.number}</span>
                {isCurrent && (
                  <span style={{ fontSize: 10, color: 'var(--primary)' }}>{t('design.versions.current')}</span>
                )}
                {typeof version.critique?.score === 'number' && (
                  <span style={{ fontSize: 10, color: 'var(--fg-muted)' }}>
                    {t('design.versions.score', { score: version.critique.score.toFixed(1) })}
                  </span>
                )}
                {!isCurrent && (
                  <button
                    onClick={() => void checkout(artifactId, version.number)}
                    title={t('design.versions.checkoutHint')}
                    style={{
                      marginLeft: 'auto',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.3rem',
                      background: 'none',
                      border: '1px solid var(--border)',
                      borderRadius: 'var(--radius-sm)',
                      padding: '0.15rem 0.4rem',
                      fontSize: 10,
                      color: 'var(--fg-muted)',
                      cursor: 'pointer',
                    }}
                  >
                    <FontAwesomeIcon icon={faRotateLeft} style={{ fontSize: 9 }} />
                    {t('design.versions.checkout')}
                  </button>
                )}
              </div>
              {version.summary && (
                <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginTop: 3 }}>{version.summary}</div>
              )}
              <div style={{ fontSize: 10, color: 'var(--fg-muted)', marginTop: 2, opacity: 0.75 }}>
                {safeDate(version.created_at)}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

/** A version row must render even if the server sent a timestamp we cannot parse. */
function safeDate(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return format(parsed, 'yyyy-MM-dd HH:mm')
}
