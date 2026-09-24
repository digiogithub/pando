export default function ProgressBar({ value, max = 100 }: { value: number; max?: number }) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100))
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-raised" role="progressbar" aria-valuenow={value} aria-valuemin={0} aria-valuemax={max}>
      <div
        className={`h-full rounded-full transition-[width] duration-300 ease-out ${pct === 100 ? 'bg-success' : 'bg-accent'}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}
