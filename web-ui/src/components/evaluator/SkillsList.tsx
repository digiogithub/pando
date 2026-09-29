import { useState } from 'react'
import type { Skill, SkillStatus } from '@pando/client/types'
import { Button } from '@/components/ui'
import EmptyState from '@/components/shared/EmptyState'
import SkillCard from './SkillCard'

interface SkillsListProps {
  skills: Skill[]
  onReview?: (id: string, decision: 'approve' | 'reject') => Promise<void>
}

const TABS: SkillStatus[] = ['pending', 'approved', 'rejected']

export default function SkillsList({ skills, onReview }: SkillsListProps) {
  const pendingCount = skills.filter((s) => s.status === 'pending').length
  const [tab, setTab] = useState<SkillStatus>(pendingCount > 0 ? 'pending' : 'approved')

  const shown = skills
    .filter((s) => s.status === tab)
    .sort((a, b) => b.success_rate - a.success_rate || b.uses - a.uses)

  return (
    <div className="flex min-w-[280px] flex-1 flex-col overflow-hidden rounded-md border border-border">
      <div className="flex items-center justify-between gap-2 border-b border-border bg-shell px-4 py-2.5 text-sm font-semibold text-fg">
        <span>Learned Skills</span>
        <div className="flex gap-1">
          {TABS.map((t) => (
            <Button key={t} size="sm" variant={tab === t ? 'secondary' : 'ghost'} onClick={() => setTab(t)}>
              {t}
              {t === 'pending' && pendingCount > 0 ? ` (${pendingCount})` : ''}
            </Button>
          ))}
        </div>
      </div>

      {shown.length === 0 ? (
        <EmptyState
          title={`No ${tab} skills`}
          description="The judge proposes skills from decisive sessions. Approved skills reach new sessions only."
        />
      ) : (
        <div className="flex-1 overflow-y-auto">
          {shown.map((skill) => (
            <SkillCard key={skill.id} skill={skill} onReview={onReview} />
          ))}
        </div>
      )}
    </div>
  )
}
