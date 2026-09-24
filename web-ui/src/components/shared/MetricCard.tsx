import { Card } from '@/components/ui'

interface MetricCardProps {
  label: string
  value: string | number
  icon?: React.ReactNode
  trend?: 'up' | 'down' | 'neutral'
  description?: string
}

export default function MetricCard({ label, value, icon, description }: MetricCardProps) {
  return (
    <Card padding="lg" className="metric-card">
      <div className="metric-label">{label}</div>
      <div className="metric-value">
        {icon}
        {value}
      </div>
      {description && <div className="metric-description">{description}</div>}
    </Card>
  )
}
