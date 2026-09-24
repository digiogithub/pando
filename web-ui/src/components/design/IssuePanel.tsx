import clsx from 'clsx'
import { useTranslation } from 'react-i18next'
import { useDesignStore, type DesignIssue } from '@pando/client/stores/designStore'
import { Button } from '@/components/ui'
import { CircleAlert, CircleCheck, CircleQuestionMark, Gavel, RotateCw, TriangleAlert } from '@/components/ui/icons'

interface IssuePanelProps {
  artifactId: string
}

const severityTone: Record<string, string> = {
  blocking: 'blocking',
  error: 'error',
  warning: 'warning',
  info: 'info',
}

const severityIcon = {
  blocking: Gavel,
  error: CircleAlert,
  warning: TriangleAlert,
  info: CircleQuestionMark,
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
      <div className="design-issue-header">
        <div className="design-issue-header-row">
          {critique ? (
            <span className="design-issue-score" style={{ color: decision?.pass ? 'var(--success)' : 'var(--warning)' }}>
              {t('design.critique.score', { score: critique.score.toFixed(1) })}
            </span>
          ) : (
            <span className="design-issue-title">{t('design.critique.title')}</span>
          )}
          {decision && (
            <span className="design-issue-verdict">
              {verdict} · {t('design.critique.round', { round: decision.round, max: decision.max_rounds })}
            </span>
          )}
          <Button
            size="sm"
            variant="primary"
            icon={<RotateCw size={11} />}
            loading={running}
            onClick={() => void runCritique(artifactId)}
            style={{ marginLeft: 'auto' }}
          >
            {running ? t('design.critique.running') : t('design.critique.run')}
          </Button>
        </div>
        {settings && (
          <div className="design-issue-policy">
            {t('design.critique.policy', { policy: settings.policy, threshold: settings.threshold.toFixed(1) })}
          </div>
        )}
        {decision?.reason && <div className="design-issue-reason">{decision.reason}</div>}
      </div>

      <div className="design-issue-list">
        {!critique ? (
          <div className="design-issue-empty">{loaded ? t('design.critique.never') : ''}</div>
        ) : critique.issues.length === 0 ? (
          <div className="design-issue-none">
            <CircleCheck size={14} />
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
  const tone = severityTone[issue.severity] ?? 'info'
  const Icon = severityIcon[issue.severity as keyof typeof severityIcon] ?? CircleQuestionMark

  return (
    <div
      onClick={onSelect}
      title={onSelect ? t('design.critique.select') : undefined}
      className={clsx('design-issue-row', `design-issue-row--${tone}`, onSelect && 'design-issue-row--clickable')}
    >
      <div className="design-issue-row-top">
        <Icon size={11} className={`tone-${tone}`} />
        {issue.code && <span className="design-issue-code">{issue.code}</span>}
        {issue.node_id && <span className="design-issue-node">{issue.node_id}</span>}
      </div>
      <div className="design-issue-message">{issue.message}</div>
      {issue.fix && (
        <div className="design-issue-fix">
          {t('design.critique.fix')}: {issue.fix}
        </div>
      )}
    </div>
  )
}
