import type { HTMLAttributes } from 'react'
import clsx from 'clsx'

/** Keyboard key hint. For combos render several: <Kbd>⌘</Kbd><Kbd>K</Kbd>. */
export function Kbd({ className, ...rest }: HTMLAttributes<HTMLElement>) {
  return <kbd className={clsx('ui-kbd', className)} {...rest} />
}
