import { InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes, useState } from 'react'

// Base label + field wrapper
function Field({ label, children }: { label: string; children: React.ReactNode }) {
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
      {children}
    </div>
  )
}

const inputStyle: React.CSSProperties = {
  background: 'var(--input-bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 14,
  padding: '0.5rem 0.75rem',
  outline: 'none',
  width: '100%',
  transition: 'border-color 0.15s',
  fontFamily: 'inherit',
  boxSizing: 'border-box',
}

export function TextInput({
  label,
  ...props
}: { label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <Field label={label}>
      <input
        {...props}
        style={inputStyle}
        onFocus={(e) => {
          e.target.style.borderColor = 'var(--border-focus)'
        }}
        onBlur={(e) => {
          e.target.style.borderColor = 'var(--border)'
        }}
      />
    </Field>
  )
}

export function SelectInput({
  label,
  options,
  ...props
}: {
  label: string
  options: { value: string; label: string }[]
} & SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <Field label={label}>
      <select
        {...props}
        style={{ ...inputStyle, cursor: 'pointer' }}
        onFocus={(e) => {
          e.currentTarget.style.borderColor = 'var(--border-focus)'
        }}
        onBlur={(e) => {
          e.currentTarget.style.borderColor = 'var(--border)'
        }}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </Field>
  )
}

export function Textarea({
  label,
  ...props
}: { label: string } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <Field label={label}>
      <textarea
        {...props}
        style={{ ...inputStyle, resize: 'vertical', minHeight: 80 }}
        onFocus={(e) => {
          e.target.style.borderColor = 'var(--border-focus)'
        }}
        onBlur={(e) => {
          e.target.style.borderColor = 'var(--border)'
        }}
      />
    </Field>
  )
}

/**
 * MaskedInput — a password-style input with a show/hide toggle.
 * Used for API keys and other sensitive fields.
 */
export function MaskedInput({
  label,
  ...props
}: { label: string } & InputHTMLAttributes<HTMLInputElement>) {
  const [visible, setVisible] = useState(false)
  return (
    <Field label={label}>
      <div style={{ position: 'relative' }}>
        <input
          {...props}
          type={visible ? 'text' : 'password'}
          style={{ ...inputStyle, paddingRight: '2.5rem' }}
          onFocus={(e) => {
            e.target.style.borderColor = 'var(--border-focus)'
          }}
          onBlur={(e) => {
            e.target.style.borderColor = 'var(--border)'
          }}
        />
        <button
          type="button"
          onClick={() => setVisible((v) => !v)}
          style={{
            position: 'absolute',
            right: '0.5rem',
            top: '50%',
            transform: 'translateY(-50%)',
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            fontSize: 13,
            padding: 0,
            fontFamily: 'inherit',
            lineHeight: 1,
          }}
          aria-label={visible ? 'Hide value' : 'Show value'}
        >
          {visible ? '🙈' : '👁'}
        </button>
      </div>
    </Field>
  )
}

export function Toggle({
  label,
  checked,
  onChange,
  description,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  description?: string
}) {
  return (
    <div
      role="switch"
      aria-checked={checked}
      tabIndex={0}
      style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', cursor: 'pointer' }}
      onClick={() => onChange(!checked)}
      onKeyDown={(e) => {
        if (e.key === ' ' || e.key === 'Enter') {
          e.preventDefault()
          onChange(!checked)
        }
      }}
    >
      {/* Track */}
      <div
        style={{
          width: 36,
          height: 20,
          borderRadius: 10,
          background: checked ? 'var(--primary)' : 'var(--border)',
          position: 'relative',
          transition: 'background 0.2s',
          flexShrink: 0,
        }}
      >
        {/* Thumb */}
        <div
          style={{
            width: 16,
            height: 16,
            borderRadius: '50%',
            background: 'white',
            position: 'absolute',
            top: 2,
            left: checked ? 18 : 2,
            transition: 'left 0.2s',
            boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
          }}
        />
      </div>
      <div>
        <div style={{ fontSize: 14, color: 'var(--fg)', fontWeight: 500 }}>{label}</div>
        {description && (
          <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>{description}</div>
        )}
      </div>
    </div>
  )
}
