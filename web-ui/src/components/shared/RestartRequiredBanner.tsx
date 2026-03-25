export default function RestartRequiredBanner() {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.5rem',
        padding: '0.625rem 0.875rem',
        background: 'rgba(217, 119, 6, 0.1)',
        border: '1px solid rgba(217, 119, 6, 0.3)',
        borderRadius: 'var(--radius-sm)',
        fontSize: 13,
        color: 'var(--fg)',
        marginBottom: '1.25rem',
      }}
    >
      <span style={{ fontSize: 16 }}>⚠</span>
      <span>Changes to this section require restarting the application to take effect.</span>
    </div>
  )
}
