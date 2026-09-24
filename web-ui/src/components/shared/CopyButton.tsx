import { useState } from 'react'
import clsx from 'clsx'
import { Copy, Check } from '@/components/ui/icons'
import { copyToClipboard } from '@/utils/clipboard'

interface CopyButtonProps {
  /**
   * Text to copy. Accepts a lazy getter instead of a plain string for
   * callers that need to read fresh content at click time (e.g. the code
   * block copy button, which reads the current DOM text of a <pre> that may
   * still be streaming).
   */
  text: string | (() => string)
  /** Idle-state button text. Defaults to "copy" (matches the original code-block button). */
  label?: string
  /** Tooltip shown before a successful copy. Defaults to "Copy". */
  title?: string
  className?: string
  /** 'sm' (default, matches the original code-block button) or 'md' for a slightly larger touch target. */
  size?: 'sm' | 'md'
  style?: React.CSSProperties
}

export default function CopyButton({
  text,
  label = 'copy',
  title = 'Copy',
  className,
  size = 'sm',
  style,
}: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    const value = typeof text === 'function' ? text() : text
    if (!value) return
    const ok = await copyToClipboard(value)
    if (ok) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const iconSize = size === 'md' ? 13 : 11

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={copied ? 'Copied!' : title}
      className={clsx(
        'inline-flex items-center gap-1 rounded-sm border font-sans transition-colors',
        size === 'md' ? 'px-2.5 py-1 text-xs' : 'px-1.5 py-0.5 text-[11px]',
        copied ? 'border-success bg-success-soft text-success' : 'border-border bg-card text-faint hover:text-fg hover:bg-raised',
        className,
      )}
      style={style}
    >
      {copied ? <Check size={iconSize} /> : <Copy size={iconSize} />}
      {copied ? 'copied' : label}
    </button>
  )
}
