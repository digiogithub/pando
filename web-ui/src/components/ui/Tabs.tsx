import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import clsx from 'clsx'

export interface TabItem<V extends string = string> {
  value: V
  label: ReactNode
  icon?: ReactNode
  disabled?: boolean
}

function useRovingKeys<V extends string>(items: TabItem<V>[], value: V, onChange: (v: V) => void) {
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const onKeyDown = (e: KeyboardEvent) => {
    const enabled = items.map((it, i) => ({ it, i })).filter((x) => !x.it.disabled)
    const cur = enabled.findIndex((x) => x.it.value === value)
    let next = -1
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (cur + 1) % enabled.length
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (cur - 1 + enabled.length) % enabled.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = enabled.length - 1
    if (next < 0) return
    e.preventDefault()
    const target = enabled[next]
    onChange(target.it.value)
    refs.current[target.i]?.focus()
  }
  return { refs, onKeyDown }
}

export interface TabsProps<V extends string = string> {
  items: TabItem<V>[]
  value: V
  onChange: (value: V) => void
  'aria-label'?: string
  className?: string
}

/** Underline tabs (role="tablist"). Render the panel yourself based on `value`. */
export function Tabs<V extends string = string>({ items, value, onChange, className, ...rest }: TabsProps<V>) {
  const { refs, onKeyDown } = useRovingKeys(items, value, onChange)
  return (
    <div role="tablist" aria-label={rest['aria-label']} className={clsx('ui-tabs', className)} onKeyDown={onKeyDown}>
      {items.map((it, i) => (
        <button
          key={it.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="tab"
          aria-selected={it.value === value}
          tabIndex={it.value === value ? 0 : -1}
          disabled={it.disabled}
          className="ui-tab"
          onClick={() => onChange(it.value)}
        >
          {it.icon}
          {it.label}
        </button>
      ))}
    </div>
  )
}

export interface SegmentedControlProps<V extends string = string> extends TabsProps<V> {
  size?: 'sm' | 'md'
}

/** Compact exclusive choice (role="radiogroup"), e.g. Light / Dark / System. */
export function SegmentedControl<V extends string = string>({
  items,
  value,
  onChange,
  size = 'md',
  className,
  ...rest
}: SegmentedControlProps<V>) {
  const { refs, onKeyDown } = useRovingKeys(items, value, onChange)
  return (
    <div
      role="radiogroup"
      aria-label={rest['aria-label']}
      className={clsx('ui-segmented', size === 'sm' && 'ui-segmented--sm', className)}
      onKeyDown={onKeyDown}
    >
      {items.map((it, i) => (
        <button
          key={it.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="radio"
          aria-checked={it.value === value}
          tabIndex={it.value === value ? 0 : -1}
          disabled={it.disabled}
          className="ui-segment"
          onClick={() => onChange(it.value)}
        >
          {it.icon}
          {it.label}
        </button>
      ))}
    </div>
  )
}
