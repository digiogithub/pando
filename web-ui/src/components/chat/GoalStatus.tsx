import { useTranslation } from 'react-i18next'
import type { GoalStatus as GoalStatusModel } from '@pando/client/types'
import { Badge, Button, type BadgeTone } from '@/components/ui'
import { Target } from '@/components/ui/icons'

interface GoalStatusProps {
  goal: GoalStatusModel | null
  loading?: boolean
  cancelling?: boolean
  onCancel?: () => void
}

const badgeTones: Record<string, BadgeTone> = {
  running: 'accent',
  completed: 'success',
  blocked: 'warning',
  cancelled: 'danger',
}

function formatTimestamp(value?: number): string | null {
  if (!value) return null
  return new Date(value * 1000).toLocaleString()
}

export default function GoalStatus({ goal, loading = false, cancelling = false, onCancel }: GoalStatusProps) {
  const { t } = useTranslation()
  if (!goal && !loading) return null

  const status = loading && !goal ? 'loading' : goal?.status ?? 'idle'
  const completedAt = formatTimestamp(goal?.completedAt)

  return (
    <div className="chat-banner">
      <div className="chat-banner-head">
        <Target size={14} className="chat-icon-idle" />
        <span className="chat-banner-title">{t('chat.goal.title')}</span>
        <Badge tone={badgeTones[goal?.status ?? ''] ?? 'neutral'} dot={goal?.status === 'running'}>
          {t(`chat.goal.status.${status}`, { defaultValue: status })}
        </Badge>
        <span className="chat-spacer" />
        {goal?.status === 'running' && onCancel && (
          <Button variant="ghost" size="sm" onClick={onCancel} loading={cancelling}>
            {cancelling ? t('chat.goal.cancelling') : t('chat.goal.cancel')}
          </Button>
        )}
      </div>

      <div className="chat-banner-body">
        {goal ? (
          <>
            <div className="chat-goal-objective">{goal.objective}</div>
            <div className="chat-goal-meta">
              <span>{t('chat.goal.iteration', { current: goal.iteration, max: goal.maxIterations })}</span>
              {goal.startedAt > 0 && <span>{t('chat.goal.started', { time: formatTimestamp(goal.startedAt) })}</span>}
              {completedAt && <span>{t('chat.goal.finished', { time: completedAt })}</span>}
            </div>
            {goal.progress && (
              <div className="chat-goal-line"><strong>{t('chat.goal.progress')}:</strong> {goal.progress}</div>
            )}
            {goal.nextStep && goal.status === 'running' && (
              <div className="chat-goal-line"><strong>{t('chat.goal.nextStep')}:</strong> {goal.nextStep}</div>
            )}
          </>
        ) : (
          <div className="chat-goal-line">{t('chat.goal.loading')}</div>
        )}
      </div>
    </div>
  )
}
