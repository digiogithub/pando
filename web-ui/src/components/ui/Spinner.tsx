import clsx from 'clsx'

export interface SpinnerProps {
  size?: number
  className?: string
  /** Accessible label; omit when the spinner is decorative (e.g. inside a busy button). */
  label?: string
}

export function Spinner({ size = 16, className, label }: SpinnerProps) {
  return (
    <svg
      className={clsx('ui-spinner', className)}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      role={label ? 'status' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="2.5" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
    </svg>
  )
}
