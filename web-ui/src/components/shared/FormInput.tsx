import { InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes, useState } from 'react'
import { Input, Select, Switch, Textarea as UITextarea, IconButton } from '@/components/ui'
import { Eye, EyeOff } from '@/components/ui/icons'

// Base label + field wrapper
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</label>
      {children}
    </div>
  )
}

export function TextInput({
  label,
  ...props
}: { label: string } & Omit<InputHTMLAttributes<HTMLInputElement>, 'size'>) {
  return (
    <Field label={label}>
      <Input {...props} />
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
} & Omit<SelectHTMLAttributes<HTMLSelectElement>, 'size'>) {
  return (
    <Field label={label}>
      <Select {...props} options={options} />
    </Field>
  )
}

export function Textarea({
  label,
  ...props
}: { label: string } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <Field label={label}>
      <UITextarea {...props} />
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
}: { label: string } & Omit<InputHTMLAttributes<HTMLInputElement>, 'size'>) {
  const [visible, setVisible] = useState(false)
  return (
    <Field label={label}>
      <div className="masked-input-wrap">
        <Input {...props} type={visible ? 'text' : 'password'} />
        <IconButton
          type="button"
          aria-label={visible ? 'Hide value' : 'Show value'}
          icon={visible ? <EyeOff size={14} /> : <Eye size={14} />}
          size="sm"
          className="masked-input-toggle"
          onClick={() => setVisible((v) => !v)}
        />
      </div>
    </Field>
  )
}

export function Toggle({
  label,
  checked,
  onChange,
  description,
  disabled,
  hint,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  description?: string
  disabled?: boolean
  /** Shown under description, e.g. why the toggle is disabled. */
  hint?: string
}) {
  return (
    <div className="flex items-center gap-3">
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} aria-label={label} />
      <div>
        <div className="text-sm font-medium text-fg">{label}</div>
        {description && <div className="text-xs text-muted">{description}</div>}
        {hint && <div className="mt-0.5 text-xs text-warning">{hint}</div>}
      </div>
    </div>
  )
}
