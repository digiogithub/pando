import { Badge } from '@/components/ui'
import type { Skill } from '@pando/client/types'
import ProgressBar from '@/components/shared/ProgressBar'

interface SkillCardProps {
  skill: Skill
}

export default function SkillCard({ skill }: SkillCardProps) {
  return (
    <div className="flex flex-col gap-1.5 border-b border-border px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="is-ellipsis overflow-hidden whitespace-nowrap font-mono text-xs font-semibold text-fg" title={skill.name}>
          {skill.name}
        </span>
        <Badge className="flex-shrink-0">{skill.uses} uses</Badge>
      </div>

      <ProgressBar value={skill.confidence} max={1} />

      {skill.description && (
        <p className="line-clamp-2 text-xs leading-snug text-muted">{skill.description}</p>
      )}
    </div>
  )
}
