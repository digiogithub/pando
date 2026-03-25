import { useState } from 'react'

interface MaskedInputProps {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  actionLabel?: string
  onAction?: () => void
}

export default function MaskedInput({
  label,
  value,
  onChange,
  placeholder,
  actionLabel,
  onAction,
}: MaskedInputProps) {
  const [visible, setVisible] = useState(false)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <label
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--fg-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
        }}
      >
        {label}
      </label>
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <div style={{ position: 'relative', flex: 1 }}>
          <input
            type={visible ? 'text' : 'password'}
            value={value}
            placeholder={placeholder}
            onChange={(e) => onChange(e.target.value)}
            style={{
              background: 'var(--input-bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--fg)',
              fontSize: 14,
              padding: '0.5rem 2.5rem 0.5rem 0.75rem',
              outline: 'none',
              width: '100%',
              fontFamily: 'inherit',
              boxSizing: 'border-box',
            }}
            onFocus={(e) => (e.target.style.borderColor = 'var(--border-focus)')}
            onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
          />
          <button
            type="button"
            onClick={() => setVisible((v) => !v)}
            title={visible ? 'Hide' : 'Show'}
            style={{
              position: 'absolute',
              right: '0.5rem',
              top: '50%',
              transform: 'translateY(-50%)',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-muted)',
              fontSize: 14,
              padding: 0,
              lineHeight: 1,
            }}
          >
            {visible ? '🙈' : '👁'}
          </button>
        </div>
        {actionLabel && onAction && (
          <button
            type="button"
            onClick={onAction}
            style={{
              padding: '0.5rem 0.875rem',
              background: 'transparent',
              color: 'var(--primary)',
              border: '1px solid var(--primary)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
              whiteSpace: 'nowrap',
              flexShrink: 0,
            }}
          >
            {actionLabel}
          </button>
        )}
      </div>
    </div>
  )
}
