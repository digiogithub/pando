export default function ProgressBar({ value, max = 100 }: { value: number; max?: number }) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100))
  return (
    <div
      style={{
        width: '100%',
        height: 6,
        background: 'var(--border)',
        borderRadius: 0,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          height: '100%',
          width: `${pct}%`,
          background: pct === 100 ? 'var(--success)' : 'var(--primary)',
          borderRadius: 0,
          transition: 'width 0.3s ease',
        }}
      />
    </div>
  )
}
