import { useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCopy, faCheck } from '@fortawesome/free-solid-svg-icons'
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

  const fontSize = size === 'md' ? 12 : 10

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={copied ? 'Copied!' : title}
      className={className}
      style={{
        background: copied ? 'color-mix(in srgb, var(--success) 15%, var(--surface))' : 'var(--surface)',
        border: `1px solid ${copied ? 'var(--success)' : 'var(--border)'}`,
        borderRadius: 'var(--radius-sm)',
        padding: size === 'md' ? '0.3rem 0.6rem' : '0.2rem 0.45rem',
        cursor: 'pointer',
        color: copied ? 'var(--success)' : 'var(--fg-dim)',
        fontSize,
        lineHeight: 1,
        display: 'inline-flex',
        alignItems: 'center',
        gap: '0.25rem',
        transition: 'all 0.15s ease',
        fontFamily: 'inherit',
        ...style,
      }}
    >
      <FontAwesomeIcon icon={copied ? faCheck : faCopy} style={{ fontSize: fontSize - 1 }} />
      {copied ? 'copied' : label}
    </button>
  )
}
