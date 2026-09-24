import { forwardRef, type ReactNode, type SelectHTMLAttributes } from 'react'
import clsx from 'clsx'
import { ChevronDown } from './icons'
import type { ControlSize } from './Button'

export interface SelectOption {
  value: string
  label: ReactNode
  disabled?: boolean
}

export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'size'> {
  size?: ControlSize
  invalid?: boolean
  /** Convenience: render these options (children are rendered after them). */
  options?: SelectOption[]
  /** Class for the wrapper (width, margins). */
  wrapperClassName?: string
}

/** Native <select> with Pando styling — keeps OS keyboard/accessibility behaviour. */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { size = 'md', invalid, options, className, wrapperClassName, children, ...rest },
  ref,
) {
  return (
    <span className={clsx('ui-select-wrap', wrapperClassName)}>
      <select
        ref={ref}
        className={clsx('ui-select', size === 'sm' && 'ui-select--sm', className)}
        aria-invalid={invalid || undefined}
        {...rest}
      >
        {options?.map((o) => (
          <option key={o.value} value={o.value} disabled={o.disabled}>
            {o.label}
          </option>
        ))}
        {children}
      </select>
      <ChevronDown className="ui-select-chevron" size={14} aria-hidden />
    </span>
  )
})
