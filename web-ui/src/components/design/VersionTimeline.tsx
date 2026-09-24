import clsx from 'clsx'
import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import { useDesignStore } from '@pando/client/stores/designStore'
import { Button } from '@/components/ui'
import { History, RotateCcw } from '@/components/ui/icons'

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
      <div className="design-versions-header">
        <History size={11} />
        <span className="design-versions-title">{t('design.versions.title')}</span>
      </div>

      <div className="design-versions-list">
        {versions.length === 0 && <div className="design-versions-empty">{t('design.versions.empty')}</div>}
        {versions.map((version) => {
          const isCurrent = version.number === currentVersion
          return (
            <div key={version.number} className={clsx('design-version-row', isCurrent && 'design-version-row--current')}>
              <div className="design-version-top">
                <span className="design-version-number">v{version.number}</span>
                {isCurrent && <span className="design-version-current-tag">{t('design.versions.current')}</span>}
                {typeof version.critique?.score === 'number' && (
                  <span className="design-version-score">{t('design.versions.score', { score: version.critique.score.toFixed(1) })}</span>
                )}
                {!isCurrent && (
                  <Button
                    size="sm"
                    variant="ghost"
                    icon={<RotateCcw size={9} />}
                    onClick={() => void checkout(artifactId, version.number)}
                    title={t('design.versions.checkoutHint')}
                    className="design-version-checkout"
                  >
                    {t('design.versions.checkout')}
                  </Button>
                )}
              </div>
              {version.summary && <div className="design-version-summary">{version.summary}</div>}
              <div className="design-version-date">{safeDate(version.created_at)}</div>
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
