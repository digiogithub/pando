import { forwardRef, useCallback, useLayoutEffect, useRef, type InputHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import clsx from 'clsx'
import type { ControlSize } from './Button'

export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> {
  size?: ControlSize
  invalid?: boolean
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { size = 'md', invalid, className, ...rest },
  ref,
) {
  return (
    <input
      ref={ref}
      className={clsx('ui-input', size === 'sm' && 'ui-input--sm', className)}
      aria-invalid={invalid || undefined}
      {...rest}
    />
  )
})

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean
  /** Grow with content (no manual resize). */
  autosize?: boolean
  /** Max height in rows when autosizing; scrolls beyond it. */
  maxRows?: number
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { invalid, autosize, maxRows = 12, className, value, onChange, ...rest },
  ref,
) {
  const innerRef = useRef<HTMLTextAreaElement | null>(null)

  const setRefs = useCallback(
    (el: HTMLTextAreaElement | null) => {
      innerRef.current = el
      if (typeof ref === 'function') ref(el)
      else if (ref) ref.current = el
    },
    [ref],
  )

  const resize = useCallback(() => {
    const el = innerRef.current
    if (!el || !autosize) return
    el.style.height = 'auto'
    const lineHeight = parseFloat(getComputedStyle(el).lineHeight) || 20
    const max = lineHeight * maxRows + 18
    const next = Math.min(el.scrollHeight + 2, max)
    el.style.height = `${next}px`
    el.style.overflowY = el.scrollHeight + 2 > max ? 'auto' : 'hidden'
  }, [autosize, maxRows])

  useLayoutEffect(resize, [resize, value])

  return (
    <textarea
      ref={setRefs}
      className={clsx('ui-textarea', autosize && 'ui-textarea--autosize', className)}
      aria-invalid={invalid || undefined}
      value={value}
      onChange={(e) => {
        onChange?.(e)
        if (value === undefined) resize()
      }}
      {...rest}
    />
  )
})
