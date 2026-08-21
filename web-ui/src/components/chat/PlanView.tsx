import { useState } from 'react'
import type { CSSProperties } from 'react'
import type { PlanEntry } from '@pando/client/hooks/useChat'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faChevronDown, faChevronRight, faCheck, faSpinner, faClock } from '@fortawesome/free-solid-svg-icons'

interface PlanViewProps {
  plan: PlanEntry[]
}

const statusIcon = (status: string) => {
  switch (status) {
    case 'completed':
      return <FontAwesomeIcon icon={faCheck} style={{ color: 'var(--success)', fontSize: 11 }} />
    case 'in_progress':
      return <FontAwesomeIcon icon={faSpinner} spin style={{ color: 'var(--primary)', fontSize: 11 }} />
    default:
      return <FontAwesomeIcon icon={faClock} style={{ color: 'var(--fg-dim)', fontSize: 11 }} />
  }
}

const panelStyle: CSSProperties = {
  margin: '0 1rem 0.5rem',
  border: '1px solid var(--border)',
  background: 'var(--bg-secondary)',
  overflow: 'hidden',
}

const headerStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.5rem',
  padding: '0.5rem 0.75rem',
  cursor: 'pointer',
  userSelect: 'none',
  fontSize: 13,
  fontWeight: 600,
  color: 'var(--fg)',
}

const itemStyle = (active: boolean): CSSProperties => ({
  display: 'flex',
  alignItems: 'center',
  gap: '0.5rem',
  padding: '0.3rem 0.75rem 0.3rem 1.5rem',
  fontSize: 13,
  color: active ? 'var(--fg)' : 'var(--fg-muted)',
  fontWeight: active ? 600 : 400,
  background: active ? 'color-mix(in srgb, var(--primary) 8%, transparent)' : 'transparent',
})

export default function PlanView({ plan }: PlanViewProps) {
  const [expanded, setExpanded] = useState(false)

  if (!plan.length) return null

  const activeIdx = plan.findIndex((e) => e.status === 'in_progress')
  const activeEntry = activeIdx >= 0 ? plan[activeIdx] : null
  const completed = plan.filter((e) => e.status === 'completed').length
  const total = plan.length

  return (
    <div style={panelStyle}>
      <div style={headerStyle} onClick={() => setExpanded(!expanded)}>
        <FontAwesomeIcon
          icon={expanded ? faChevronDown : faChevronRight}
          style={{ fontSize: 10, width: 10 }}
        />
        <span
          style={{
            fontSize: 11,
            letterSpacing: '0.08em',
            textTransform: 'uppercase',
            color: 'var(--fg-dim)',
          }}
        >
          Plan
        </span>
        <span style={{ fontSize: 12, color: 'var(--fg-muted)', fontWeight: 400 }}>
          {completed}/{total}
        </span>
        {!expanded && activeEntry && (
          <span style={{ fontSize: 13, color: 'var(--primary)', fontWeight: 500, marginLeft: '0.25rem' }}>
            — {activeEntry.title}
          </span>
        )}
      </div>
      {expanded && (
        <div style={{ paddingBottom: '0.35rem' }}>
          {plan.map((entry, i) => (
            <div key={i} style={itemStyle(entry.status === 'in_progress')}>
              {statusIcon(entry.status)}
              <span style={{
                textDecoration: entry.status === 'completed' ? 'line-through' : 'none',
                opacity: entry.status === 'completed' ? 0.6 : 1,
              }}>
                {entry.title}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
