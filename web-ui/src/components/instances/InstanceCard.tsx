import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faServer, faStar } from '@fortawesome/free-solid-svg-icons'
import type { InstanceInfo } from '@/stores/instancesStore'

/** Replace leading /home/<user> or /Users/<user> with ~. */
function shortenPath(path: string): string {
  return path
    .replace(/^\/home\/[^/]+/, '~')
    .replace(/^\/Users\/[^/]+/, '~')
}

function modeColor(mode: string): string {
  switch (mode) {
    case 'tui': return 'var(--primary)'
    case 'webui': return '#4a9eff'
    case 'desktop': return '#a78bfa'
    case 'acp': return '#34d399'
    default: return 'var(--fg-muted)'
  }
}

interface InstanceCardProps {
  instance: InstanceInfo
  selected: boolean
  onClick: () => void
}

export default function InstanceCard({ instance, selected, onClick }: InstanceCardProps) {
  return (
    <div
      onClick={onClick}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '0.375rem',
        padding: '0.75rem 1rem',
        borderBottom: '1px solid var(--border)',
        background: selected ? 'var(--selected)' : 'transparent',
        cursor: 'pointer',
        transition: 'background 0.1s',
        borderLeft: selected ? '3px solid var(--primary)' : '3px solid transparent',
      }}
      onMouseEnter={(e) => {
        if (!selected) (e.currentTarget as HTMLDivElement).style.background = 'var(--sidebar-bg)'
      }}
      onMouseLeave={(e) => {
        if (!selected) (e.currentTarget as HTMLDivElement).style.background = 'transparent'
      }}
    >
      {/* Top row: icon + path + primary badge */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        <FontAwesomeIcon
          icon={faServer}
          style={{ fontSize: 12, color: selected ? 'var(--primary)' : 'var(--fg-muted)', flexShrink: 0 }}
        />
        <span
          style={{
            flex: 1,
            fontSize: 12,
            fontWeight: selected ? 600 : 400,
            color: 'var(--fg)',
            fontFamily: 'monospace',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={instance.path}
        >
          {shortenPath(instance.path)}
        </span>
        {instance.is_primary && (
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.25rem',
              fontSize: 10,
              fontWeight: 700,
              color: '#f59e0b',
              background: 'rgba(245, 158, 11, 0.15)',
              borderRadius: 'var(--radius-sm)',
              padding: '0.15rem 0.4rem',
              border: '1px solid rgba(245, 158, 11, 0.4)',
              whiteSpace: 'nowrap',
            }}
          >
            <FontAwesomeIcon icon={faStar} style={{ fontSize: 8 }} />
            PRIMARY
          </span>
        )}
      </div>

      {/* Bottom row: mode badge + PID */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', paddingLeft: '1.25rem' }}>
        <span
          style={{
            fontSize: 10,
            fontWeight: 600,
            color: modeColor(instance.mode),
            background: 'var(--sidebar-bg)',
            borderRadius: 'var(--radius-sm)',
            padding: '0.15rem 0.4rem',
            border: `1px solid ${modeColor(instance.mode)}`,
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
          }}
        >
          {instance.mode}
        </span>
        <span style={{ fontSize: 11, color: 'var(--fg-muted)' }}>
          PID {instance.pid}
        </span>
        <span style={{ fontSize: 10, color: 'var(--fg-dim)', marginLeft: 'auto' }}>
          {instance.instance_id.slice(0, 8)}
        </span>
      </div>
    </div>
  )
}
