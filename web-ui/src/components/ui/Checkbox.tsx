import { forwardRef, useEffect, useRef, type InputHTMLAttributes, type ReactNode } from 'react'
import clsx from 'clsx'
import { Check, Minus } from './icons'

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  indeterminate?: boolean
  label?: ReactNode
}

export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { checked, onCheckedChange, indeterminate = false, label, className, disabled, ...rest },
  ref,
) {
  const innerRef = useRef<HTMLInputElement | null>(null)

  useEffect(() => {
    if (innerRef.current) innerRef.current.indeterminate = indeterminate
  }, [indeterminate])

  return (
    <label className={clsx('ui-check', disabled && 'ui-check--disabled', className)}>
      <input
        ref={(el) => {
          innerRef.current = el
          if (typeof ref === 'function') ref(el)
          else if (ref) ref.current = el
        }}
        type="checkbox"
        className="ui-check-input"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onCheckedChange(e.target.checked)}
        {...rest}
      />
      <span className="ui-check-box" aria-hidden>
        {indeterminate ? <Minus size={12} strokeWidth={2.5} /> : <Check size={12} strokeWidth={2.5} />}
      </span>
      {label != null && <span>{label}</span>}
    </label>
  )
})
