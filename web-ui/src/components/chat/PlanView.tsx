import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { PlanEntry } from '@pando/client/hooks/useChat'
import { Spinner } from '@/components/ui'
import { ChevronRight, CircleCheck, Circle, ListChecks } from '@/components/ui/icons'

interface PlanViewProps {
  plan: PlanEntry[]
}

export function PlanStatusIcon({ status, size = 14 }: { status: string; size?: number }) {
  switch (status) {
    case 'completed':
      return <CircleCheck size={size} className="chat-icon-done" />
    case 'in_progress':
      return <Spinner size={size} className="chat-icon-live" />
    default:
      return <Circle size={size} className="chat-icon-idle" />
  }
}

export default function PlanView({ plan }: PlanViewProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)

  if (!plan.length) return null

  const activeEntry = plan.find((e) => e.status === 'in_progress') ?? null
  const completed = plan.filter((e) => e.status === 'completed').length

  return (
    <div className="chat-banner">
      <button type="button" className="chat-banner-head" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
        <ChevronRight size={14} className="chat-chevron chat-icon-idle" />
        <ListChecks size={14} className="chat-icon-idle" />
        <span className="chat-banner-title">{t('chat.info.plan')}</span>
        <span className="chat-banner-count">{completed}/{plan.length}</span>
        {!expanded && activeEntry && <span className="chat-banner-current">· {activeEntry.title}</span>}
      </button>
      {expanded && (
        <div className="chat-plan-list">
          {plan.map((entry, i) => (
            <div key={i} className={`chat-plan-item chat-plan-item--${entry.status}`}>
              <PlanStatusIcon status={entry.status} />
              <span className="chat-plan-text" title={entry.title}>{entry.title}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
