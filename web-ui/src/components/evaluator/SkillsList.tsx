import type { Skill } from '@pando/client/types'
import EmptyState from '@/components/shared/EmptyState'
import SkillCard from './SkillCard'

interface SkillsListProps {
  skills: Skill[]
}

export default function SkillsList({ skills }: SkillsListProps) {
  const top = [...skills].sort((a, b) => b.confidence - a.confidence).slice(0, 10)

  return (
    <div className="flex min-w-[280px] flex-1 flex-col overflow-hidden rounded-md border border-border">
      <div className="border-b border-border bg-shell px-4 py-2.5 text-sm font-semibold text-fg">
        Top Skills / Learned
      </div>

      {top.length === 0 ? (
        <EmptyState title="No skills learned yet" description="Skills are discovered as self-improvement processes sessions." />
      ) : (
        <div className="flex-1 overflow-y-auto">
          {top.map((skill) => (
            <SkillCard key={skill.id} skill={skill} />
          ))}
        </div>
      )}
    </div>
  )
}
