import { useState } from 'react'
import { Badge, Button } from '@/components/ui'
import type { BadgeTone } from '@/components/ui'
import type { Skill } from '@pando/client/types'
import ProgressBar from '@/components/shared/ProgressBar'

interface SkillCardProps {
  skill: Skill
  onReview?: (id: string, decision: 'approve' | 'reject') => Promise<void>
}

const STATUS_TONE: Record<string, BadgeTone> = {
  pending: 'warning',
  approved: 'success',
  rejected: 'neutral',
}

export default function SkillCard({ skill, onReview }: SkillCardProps) {
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null)

  const review = async (decision: 'approve' | 'reject') => {
    if (!onReview) return
    setBusy(decision)
    try {
      await onReview(skill.id, decision)
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="flex flex-col gap-1.5 border-b border-border px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="is-ellipsis overflow-hidden whitespace-nowrap font-mono text-xs font-semibold text-fg" title={skill.id}>
          {skill.name}
        </span>
        <div className="flex flex-shrink-0 items-center gap-1.5">
          <Badge tone={STATUS_TONE[skill.status] ?? 'neutral'}>{skill.status}</Badge>
          <Badge>{skill.uses} uses</Badge>
        </div>
      </div>

      {skill.status === 'approved' && skill.eval_count > 0 && (
        <ProgressBar value={skill.success_rate} max={1} />
      )}

      {skill.description && (
        <p className="line-clamp-3 text-xs leading-snug text-muted">{skill.description}</p>
      )}

      {skill.status === 'pending' && onReview && (
        <div className="flex gap-2 pt-1">
          <Button size="sm" variant="primary" loading={busy === 'approve'} disabled={busy !== null} onClick={() => review('approve')}>
            Approve
          </Button>
          <Button size="sm" variant="ghost" loading={busy === 'reject'} disabled={busy !== null} onClick={() => review('reject')}>
            Reject
          </Button>
        </div>
      )}
      {skill.status === 'approved' && onReview && (
        <div className="flex gap-2 pt-1">
          <Button size="sm" variant="ghost" loading={busy === 'reject'} disabled={busy !== null} onClick={() => review('reject')}>
            Reject
          </Button>
        </div>
      )}
    </div>
  )
}
