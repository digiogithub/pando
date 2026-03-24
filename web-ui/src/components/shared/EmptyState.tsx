interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
}

export default function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '3rem 2rem',
        gap: '0.75rem',
        color: 'var(--fg-muted)',
        textAlign: 'center',
      }}
    >
      {icon && <div style={{ fontSize: 32, marginBottom: '0.5rem' }}>{icon}</div>}
      <p style={{ fontWeight: 600, fontSize: 15, color: 'var(--fg)' }}>{title}</p>
      {description && <p style={{ fontSize: 13 }}>{description}</p>}
      {action && <div style={{ marginTop: '0.5rem' }}>{action}</div>}
    </div>
  )
}
