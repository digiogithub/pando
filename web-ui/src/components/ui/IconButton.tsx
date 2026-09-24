import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import clsx from 'clsx'
import type { ButtonVariant, ControlSize } from './Button'
import { Spinner } from './Spinner'
import { Tooltip } from './Tooltip'

export interface IconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-label'> {
  /** Required: icon-only buttons need an accessible name. */
  'aria-label': string
  icon: ReactNode
  variant?: ButtonVariant
  size?: ControlSize
  loading?: boolean
  /** Tooltip text; `true` reuses the aria-label. */
  tooltip?: ReactNode | true
  /** Pressed/active state for toggle buttons (sets aria-pressed). */
  active?: boolean
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { icon, variant = 'ghost', size = 'md', loading, tooltip, active, className, disabled, type = 'button', ...rest },
  ref,
) {
  const button = (
    <button
      ref={ref}
      type={type}
      className={clsx(
        'ui-btn',
        'ui-btn--icon',
        `ui-btn--${variant}`,
        size === 'sm' && 'ui-btn--sm',
        active && 'ui-btn--active',
        className,
      )}
      disabled={disabled || loading}
      aria-pressed={active}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <Spinner size={size === 'sm' ? 14 : 16} /> : icon}
    </button>
  )
  if (!tooltip) return button
  return <Tooltip content={tooltip === true ? rest['aria-label'] : tooltip}>{button}</Tooltip>
})
