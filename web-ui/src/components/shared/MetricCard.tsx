interface MetricCardProps {
  label: string
  value: string | number
  icon?: React.ReactNode
  trend?: 'up' | 'down' | 'neutral'
  description?: string
}

export default function MetricCard({ label, value, icon, description }: MetricCardProps) {
  return (
    <div
      style={{
        background: 'var(--card-bg)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md)',
        padding: '1.25rem 1.5rem',
        display: 'flex',
        flexDirection: 'column',
        gap: '0.375rem',
        flex: 1,
      }}
    >
      <div
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--fg-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.05em',
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontSize: 32,
          fontWeight: 700,
          color: 'var(--primary)',
          lineHeight: 1,
          display: 'flex',
          alignItems: 'center',
        }}
      >
        {icon && <span style={{ marginRight: '0.5rem', fontSize: 24 }}>{icon}</span>}
        {value}
      </div>
      {description && (
        <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>{description}</div>
      )}
    </div>
  )
}
