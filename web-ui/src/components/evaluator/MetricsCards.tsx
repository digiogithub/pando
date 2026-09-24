import { FlaskConical, Layers, Trophy } from '@/components/ui/icons'
import type { EvaluatorMetrics } from '@pando/client/types'
import MetricCard from '@/components/shared/MetricCard'

interface MetricsCardsProps {
  metrics: EvaluatorMetrics | null
}

export default function MetricsCards({ metrics }: MetricsCardsProps) {
  return (
    <div className="view-metrics" style={{ opacity: metrics ? 1 : 0.4 }}>
      <MetricCard
        label="Sessions Evaluated"
        value={metrics?.total_sessions ?? 0}
        icon={<FlaskConical size={20} />}
        description="Total sessions scored by self-improvement"
      />
      <MetricCard
        label="Prompt Templates"
        value={metrics?.total_templates ?? 0}
        icon={<Layers size={20} />}
        description="Active templates in the UCB pool"
      />
      <MetricCard
        label="Avg Reward Score"
        value={metrics ? metrics.avg_reward.toFixed(2) : '0.00'}
        icon={<Trophy size={20} />}
        description="Mean reward across all evaluated sessions"
      />
    </div>
  )
}
