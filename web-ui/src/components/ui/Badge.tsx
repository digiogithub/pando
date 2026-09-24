import type { HTMLAttributes, ReactNode } from 'react'
import clsx from 'clsx'

export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info'

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: BadgeTone
  outline?: boolean
  /** Leading status dot (colour never carries meaning alone: keep the text). */
  dot?: boolean
  icon?: ReactNode
}

export function Badge({ tone = 'neutral', outline, dot, icon, className, children, ...rest }: BadgeProps) {
  return (
    <span
      className={clsx('ui-badge', tone !== 'neutral' && `ui-badge--${tone}`, outline && 'ui-badge--outline', className)}
      {...rest}
    >
      {dot && <span className="ui-badge-dot" aria-hidden />}
      {icon}
      {children}
    </span>
  )
}
