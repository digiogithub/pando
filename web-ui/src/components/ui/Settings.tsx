import type { ReactNode } from 'react'
import clsx from 'clsx'

export interface SettingsSectionProps {
  title?: ReactNode
  description?: ReactNode
  children: ReactNode
  className?: string
}

/** A titled group of SettingsRow items rendered as one rounded card with hairline separators. */
export function SettingsSection({ title, description, children, className }: SettingsSectionProps) {
  return (
    <section className={clsx('ui-settings-section', className)}>
      {(title || description) && (
        <header className="ui-settings-section-header">
          {title && <h3 className="ui-settings-section-title">{title}</h3>}
          {description && <p className="ui-settings-section-description">{description}</p>}
        </header>
      )}
      <div className="ui-settings-group">{children}</div>
    </section>
  )
}

export interface SettingsRowProps {
  label: ReactNode
  description?: ReactNode
  /** id of the control, so clicking the label focuses/toggles it. */
  htmlFor?: string
  /** Put the control under the text (for wide controls: textareas, pickers). */
  stacked?: boolean
  children?: ReactNode
  className?: string
}

/** Label + description on the left, control on the right (macOS / Zeron settings style). */
export function SettingsRow({ label, description, htmlFor, stacked, children, className }: SettingsRowProps) {
  return (
    <div className={clsx('ui-settings-row', stacked && 'ui-settings-row--stacked', className)}>
      <div className="ui-settings-row-text">
        {htmlFor ? (
          <label className="ui-settings-row-label" htmlFor={htmlFor}>{label}</label>
        ) : (
          <span className="ui-settings-row-label">{label}</span>
        )}
        {description && <p className="ui-settings-row-description">{description}</p>}
      </div>
      {children != null && <div className="ui-settings-row-control">{children}</div>}
    </div>
  )
}
