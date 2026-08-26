import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faCircleCheck,
  faTriangleExclamation,
  faCircleExclamation,
  faCircleInfo,
  faGavel,
  faRotate,
} from '@fortawesome/free-solid-svg-icons'
import { useDesignStore, type DesignIssue } from '@pando/client/stores/designStore'

interface IssuePanelProps {
  artifactId: string
}

const severityColor: Record<string, string> = {
  blocking: 'var(--error, #e5484d)',
  error: 'var(--error, #e5484d)',
  warning: 'var(--warning, #d97706)',
  info: 'var(--fg-muted)',
}

const severityIcon = {
  blocking: faGavel,
  error: faCircleExclamation,
  warning: faTriangleExclamation,
  info: faCircleInfo,
} as const

/**
 * IssuePanel is the readable end of the quality gate: the findings of the last
 * critic pass, worst first, each one clickable back to the element it is about.
 *
 * A finding with a node id is a selection waiting to happen — that is what makes
 * the list actionable rather than a report. Findings with no node (a missing
 * title, a failed request) are still listed; they are about the document.
 */
export default function IssuePanel({ artifactId }: IssuePanelProps) {
  const { t } = useTranslation()
  const critique = useDesignStore((s) => s.critique)
  const decision = useDesignStore((s) => s.critiqueDecision)
  const settings = useDesignStore((s) => s.critiqueSettings)
  const running = useDesignStore((s) => s.critiqueRunning)
  const loaded = useDesignStore((s) => s.critiqueLoaded)
  const runCritique = useDesignStore((s) => s.runCritique)
  const setSelection = useDesignStore((s) => s.setSelection)

  const verdict = decision?.pass
    ? t('design.critique.pass')
    : decision?.iterate
      ? t('design.critique.iterate')
      : t('design.critique.stop')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div style={{ padding: '0.6rem 0.75rem', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          {critique ? (
            <span
              style={{
                fontSize: 15,
                fontWeight: 700,
                color: decision?.pass ? 'var(--success, #30a46c)' : 'var(--warning, #d97706)',
              }}
            >
              {t('design.critique.score', { score: critique.score.toFixed(1) })}
            </span>
          ) : (
            <span style={{ fontSize: 12, fontWeight: 600 }}>{t('design.critique.title')}</span>
          )}
          {decision && (
            <span style={{ fontSize: 11, color: 'var(--fg-muted)' }}>
              {verdict} · {t('design.critique.round', { round: decision.round, max: decision.max_rounds })}
            </span>
          )}
          <button
            onClick={() => void runCritique(artifactId)}
            disabled={running}
            style={{
              marginLeft: 'auto',
              display: 'flex',
              alignItems: 'center',
              gap: '0.35rem',
              padding: '0.25rem 0.5rem',
              fontSize: 11,
              background: 'var(--primary)',
              color: 'white',
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              cursor: running ? 'default' : 'pointer',
              opacity: running ? 0.6 : 1,
            }}
          >
            <FontAwesomeIcon icon={faRotate} spin={running} style={{ fontSize: 10 }} />
            {running ? t('design.critique.running') : t('design.critique.run')}
          </button>
        </div>
        {settings && (
          <div style={{ fontSize: 10.5, color: 'var(--fg-muted)', marginTop: 4 }}>
            {t('design.critique.policy', {
              policy: settings.policy,
              threshold: settings.threshold.toFixed(1),
            })}
          </div>
        )}
        {decision?.reason && (
          <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginTop: 4, lineHeight: 1.5 }}>
            {decision.reason}
          </div>
        )}
      </div>

      <div style={{ flex: 1, overflow: 'auto' }}>
        {!critique ? (
          <div style={{ padding: '1rem 0.75rem', fontSize: 12, color: 'var(--fg-muted)', lineHeight: 1.6 }}>
            {loaded ? t('design.critique.never') : ''}
          </div>
        ) : critique.issues.length === 0 ? (
          <div
            style={{
              padding: '1rem 0.75rem',
              fontSize: 12,
              color: 'var(--fg-muted)',
              lineHeight: 1.6,
              display: 'flex',
              gap: '0.5rem',
              alignItems: 'flex-start',
            }}
          >
            <FontAwesomeIcon icon={faCircleCheck} style={{ color: 'var(--success, #30a46c)', marginTop: 2 }} />
            {t('design.critique.none')}
          </div>
        ) : (
          critique.issues.map((issue, index) => (
            <IssueRow
              key={`${issue.code ?? 'critic'}-${issue.node_id ?? ''}-${index}`}
              issue={issue}
              onSelect={
                issue.node_id
                  ? () =>
                      setSelection({
                        nodeId: issue.node_id as string,
                        selection: `design://${issue.node_id}`,
                        slide: issue.slide,
                      })
                  : undefined
              }
            />
          ))
        )}
      </div>
    </div>
  )
}

function IssueRow({ issue, onSelect }: { issue: DesignIssue; onSelect?: () => void }) {
  const { t } = useTranslation()
  const color = severityColor[issue.severity] ?? 'var(--fg-muted)'
  const icon = severityIcon[issue.severity] ?? faCircleInfo

  return (
    <div
      onClick={onSelect}
      title={onSelect ? t('design.critique.select') : undefined}
      style={{
        padding: '0.45rem 0.75rem',
        borderBottom: '1px solid var(--border)',
        borderLeft: `2px solid ${color}`,
        cursor: onSelect ? 'pointer' : 'default',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.4rem' }}>
        <FontAwesomeIcon icon={icon} style={{ fontSize: 10, color }} />
        {issue.code && (
          <span style={{ fontSize: 10, color: 'var(--fg-muted)', fontFamily: "'JetBrains Mono', monospace" }}>
            {issue.code}
          </span>
        )}
        {issue.node_id && <span style={{ fontSize: 10, color: 'var(--primary)' }}>{issue.node_id}</span>}
      </div>
      <div style={{ fontSize: 11.5, marginTop: 3, lineHeight: 1.5 }}>{issue.message}</div>
      {issue.fix && (
        <div style={{ fontSize: 11, marginTop: 3, color: 'var(--fg-muted)', lineHeight: 1.5 }}>
          {t('design.critique.fix')}: {issue.fix}
        </div>
      )}
    </div>
  )
}
