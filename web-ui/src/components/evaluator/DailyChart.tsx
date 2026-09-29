import type { EvaluatorDailyMetric } from '@pando/client/types'

interface DailyChartProps {
  daily: EvaluatorDailyMetric[]
}

/** Evaluations per day (bars) with the mean reward of that day in the tooltip. */
export default function DailyChart({ daily }: DailyChartProps) {
  const max = Math.max(1, ...daily.map((d) => d.evaluations))
  return (
    <div className="mx-6 mt-3 rounded-md border border-border p-3">
      <div className="mb-2 text-xs font-semibold text-muted">
        Evaluations per day (last {daily.length} days). Hover a bar for the day's mean reward.
      </div>
      <div className="flex h-16 items-end gap-1" role="img" aria-label="Evaluations per day">
        {daily.map((d) => (
          <div
            key={d.day}
            className="flex-1 rounded-sm bg-accent"
            style={{ height: `${d.evaluations === 0 ? 2 : Math.max(6, (d.evaluations / max) * 100)}%`, opacity: d.evaluations === 0 ? 0.25 : 1 }}
            title={`${d.day}: ${d.evaluations} evaluations, mean reward ${d.avg_reward.toFixed(2)}, judge ${d.judge_calls} calls`}
          />
        ))}
      </div>
      <div className="mt-1 flex justify-between text-[10px] text-muted">
        <span>{daily[0]?.day}</span>
        <span>{daily[daily.length - 1]?.day}</span>
      </div>
    </div>
  )
}
