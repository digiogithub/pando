import type { Skill } from '@/types'
import ProgressBar from '@/components/shared/ProgressBar'

interface SkillCardProps {
  skill: Skill
}

export default function SkillCard({ skill }: SkillCardProps) {
  return (
    <div
      style={{
        padding: '0.75rem 1rem',
        borderBottom: '1px solid var(--border)',
        display: 'flex',
        flexDirection: 'column',
        gap: '0.375rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.5rem' }}>
        <span
          style={{
            fontFamily: 'monospace',
            fontSize: 12,
            fontWeight: 600,
            color: 'var(--fg)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={skill.name}
        >
          {skill.name}
        </span>
        <span
          style={{
            flexShrink: 0,
            fontSize: 11,
            fontWeight: 600,
            padding: '0.1rem 0.4rem',
            borderRadius: 9999,
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            color: 'var(--fg-muted)',
          }}
        >
          {skill.uses} uses
        </span>
      </div>

      <ProgressBar value={skill.confidence} max={1} />

      {skill.description && (
        <p
          style={{
            fontSize: 12,
            color: 'var(--fg-muted)',
            lineHeight: 1.4,
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
        >
          {skill.description}
        </p>
      )}
    </div>
  )
}
