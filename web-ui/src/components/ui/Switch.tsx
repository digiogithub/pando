import { forwardRef, type ButtonHTMLAttributes } from 'react'
import clsx from 'clsx'

export interface SwitchProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange' | 'value'> {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}

/** Toggle switch (role="switch"). Pair with a visible label via aria-labelledby or SettingsRow htmlFor/id. */
export const Switch = forwardRef<HTMLButtonElement, SwitchProps>(function Switch(
  { checked, onCheckedChange, className, disabled, onClick, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      className={clsx('ui-switch', className)}
      onClick={(e) => {
        onClick?.(e)
        if (!e.defaultPrevented) onCheckedChange(!checked)
      }}
      {...rest}
    />
  )
})
