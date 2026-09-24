import type { ReactNode } from 'react'
import clsx from 'clsx'

export interface EmptyStateProps {
  icon?: ReactNode
  title: ReactNode
  description?: ReactNode
  /** Usually a <Button>. */
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={clsx('ui-empty', className)}>
      {icon && <div className="ui-empty-icon" aria-hidden>{icon}</div>}
      <div className="ui-empty-title">{title}</div>
      {description && <div className="ui-empty-description">{description}</div>}
      {action && <div className="ui-empty-action">{action}</div>}
    </div>
  )
}
