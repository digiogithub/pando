import { forwardRef, type HTMLAttributes } from 'react'
import clsx from 'clsx'

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  padding?: 'none' | 'sm' | 'md' | 'lg'
  elevated?: boolean
  /** Hover affordance for clickable cards (add onClick + role/tabIndex yourself, or wrap a button). */
  interactive?: boolean
}

export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { padding = 'md', elevated, interactive, className, ...rest },
  ref,
) {
  return (
    <div
      ref={ref}
      className={clsx(
        'ui-card',
        padding !== 'none' && `ui-card--pad-${padding}`,
        elevated && 'ui-card--elevated',
        interactive && 'ui-card--interactive',
        className,
      )}
      {...rest}
    />
  )
})
