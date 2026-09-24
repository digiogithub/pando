import { EmptyState as UIEmptyState } from '@/components/ui'

interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
}

/** Thin backward-compatible wrapper around the `ui` EmptyState primitive. */
export default function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return <UIEmptyState icon={icon} title={title} description={description} action={action} />
}
