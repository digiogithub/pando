import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faFlask, faLayerGroup, faTrophy } from '@fortawesome/free-solid-svg-icons'
import type { EvaluatorMetrics } from '@/types'
import MetricCard from '@/components/shared/MetricCard'

interface MetricsCardsProps {
  metrics: EvaluatorMetrics | null
}

export default function MetricsCards({ metrics }: MetricsCardsProps) {
  const opacity = metrics ? 1 : 0.4

  return (
    <div
      style={{
        display: 'flex',
        gap: '1rem',
        padding: '1.5rem',
        flexWrap: 'wrap',
        opacity,
      }}
    >
      <MetricCard
        label="Sessions Evaluated"
        value={metrics?.total_sessions ?? 0}
        icon={<FontAwesomeIcon icon={faFlask} />}
        description="Total sessions scored by the evaluator"
      />
      <MetricCard
        label="Prompt Templates"
        value={metrics?.total_templates ?? 0}
        icon={<FontAwesomeIcon icon={faLayerGroup} />}
        description="Active templates in the UCB pool"
      />
      <MetricCard
        label="Avg Reward Score"
        value={metrics ? metrics.avg_reward.toFixed(2) : '0.00'}
        icon={<FontAwesomeIcon icon={faTrophy} />}
        description="Mean reward across all evaluated sessions"
      />
    </div>
  )
}
