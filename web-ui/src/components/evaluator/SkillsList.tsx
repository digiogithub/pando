import type { Skill } from '@/types'
import EmptyState from '@/components/shared/EmptyState'
import SkillCard from './SkillCard'

interface SkillsListProps {
  skills: Skill[]
}

export default function SkillsList({ skills }: SkillsListProps) {
  const top = [...skills].sort((a, b) => b.confidence - a.confidence).slice(0, 10)

  return (
    <div
      style={{
        flex: 1,
        minWidth: 280,
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md)',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        style={{
          padding: '0.75rem 1rem',
          fontWeight: 600,
          fontSize: 13,
          color: 'var(--fg)',
          borderBottom: '1px solid var(--border)',
          background: 'var(--surface)',
        }}
      >
        Top Skills / Learned
      </div>

      {top.length === 0 ? (
        <EmptyState
          title="No skills learned yet"
          description="Skills are discovered as the evaluator processes sessions."
        />
      ) : (
        <div style={{ overflowY: 'auto', flex: 1 }}>
          {top.map((skill) => (
            <SkillCard key={skill.id} skill={skill} />
          ))}
        </div>
      )}
    </div>
  )
}
