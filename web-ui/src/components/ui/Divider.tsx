import clsx from 'clsx'

export interface DividerProps {
  vertical?: boolean
  className?: string
}

export function Divider({ vertical, className }: DividerProps) {
  return (
    <hr
      className={clsx('ui-divider', vertical && 'ui-divider--vertical', className)}
      aria-orientation={vertical ? 'vertical' : 'horizontal'}
    />
  )
}
