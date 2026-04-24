import { useTranslation } from 'react-i18next'
import type { ThemeName, ThemeMode } from '@/hooks/useTheme'

interface ThemeDef {
  name: ThemeName
  label: string
  light: { bg: string; sidebar: string; primary: string; fg: string; border: string }
  dark: { bg: string; sidebar: string; primary: string; fg: string; border: string }
}

const THEMES: ThemeDef[] = [
  {
    name: 'pando',
    label: 'Pando',
    light: { bg: '#FCFAF2', sidebar: '#F5F2E8', primary: '#D4AF37', fg: '#1A1A1A', border: '#C8C5B9' },
    dark:  { bg: '#121214', sidebar: '#0A0A0C', primary: '#D4AF37', fg: '#E0E0E0', border: '#2C2C2E' },
  },
  {
    name: 'claude',
    label: 'Claude',
    light: { bg: '#f5f4ed', sidebar: '#faf9f5', primary: '#c96442', fg: '#141413', border: '#e8e6dc' },
    dark:  { bg: '#141413', sidebar: '#0f0f0e', primary: '#c96442', fg: '#faf9f5', border: '#30302e' },
  },
  {
    name: 'clay',
    label: 'Clay',
    light: { bg: '#faf9f7', sidebar: '#f4f0e8', primary: '#fbbd41', fg: '#111110', border: '#dad4c8' },
    dark:  { bg: '#1a1917', sidebar: '#111110', primary: '#fbbd41', fg: '#f5f2ee', border: '#3a3730' },
  },
  {
    name: 'starbucks',
    label: 'Starbucks',
    light: { bg: '#f2f0eb', sidebar: '#edebe9', primary: '#00754a', fg: '#1a1a18', border: '#cdc9bf' },
    dark:  { bg: '#1e3932', sidebar: '#162c27', primary: '#00a862', fg: '#f2f0eb', border: '#2b5148' },
  },
]

interface Props {
  value: string
  onChange: (themeId: string) => void
}

export default function ThemePicker({ value, onChange }: Props) {
  const { t } = useTranslation()
  const parts = value.split('-')
  const currentMode: ThemeMode = parts[parts.length - 1] === 'dark' ? 'dark' : 'light'
  const currentName = parts.slice(0, -1).join('-') as ThemeName

  return (
    <div>
      {/* Mode toggle */}
      <div style={{ display: 'flex', gap: 6, marginBottom: 14 }}>
        {(['light', 'dark'] as ThemeMode[]).map((mode) => (
          <button
            key={mode}
            onClick={() => onChange(`${currentName}-${mode}`)}
            style={{
              padding: '4px 14px',
              fontSize: 12,
              fontWeight: 600,
              fontFamily: 'inherit',
              cursor: 'pointer',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              background: currentMode === mode ? 'var(--primary)' : 'transparent',
              color: currentMode === mode ? 'var(--primary-fg)' : 'var(--fg-muted)',
              transition: 'background 0.15s, color 0.15s',
            }}
          >
            {mode === 'light' ? `☀ ${t('settings.general.themeLight')}` : `☾ ${t('settings.general.themeDark')}`}
          </button>
        ))}
      </div>

      {/* Theme cards grid */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
          gap: 10,
        }}
      >
        {THEMES.map((theme) => {
          const themeId = `${theme.name}-${currentMode}`
          const selected = currentName === theme.name
          const palette = currentMode === 'dark' ? theme.dark : theme.light

          return (
            <button
              key={theme.name}
              onClick={() => onChange(themeId)}
              style={{
                cursor: 'pointer',
                border: selected
                  ? '2px solid var(--primary)'
                  : '2px solid var(--border)',
                borderRadius: 8,
                padding: 0,
                overflow: 'hidden',
                background: 'transparent',
                outline: selected ? '3px solid var(--selected)' : 'none',
                transition: 'border-color 0.15s, outline 0.15s',
              }}
            >
              {/* Mini preview */}
              <div style={{ display: 'flex', height: 72, background: palette.bg }}>
                {/* Sidebar strip */}
                <div style={{ width: 32, background: palette.sidebar, flexShrink: 0 }}>
                  {[0, 1, 2, 3].map((i) => (
                    <div
                      key={i}
                      style={{
                        margin: '6px 5px 0',
                        height: 4,
                        borderRadius: 2,
                        background: i === 0 ? palette.primary : palette.border,
                        opacity: i === 0 ? 1 : 0.6,
                      }}
                    />
                  ))}
                </div>
                {/* Content area */}
                <div style={{ flex: 1, padding: '6px 7px', display: 'flex', flexDirection: 'column', gap: 4 }}>
                  {/* Header bar */}
                  <div style={{ height: 10, borderRadius: 2, background: palette.primary, width: '60%' }} />
                  {/* Text lines */}
                  {[0.9, 0.7, 0.5].map((op, i) => (
                    <div
                      key={i}
                      style={{
                        height: 4,
                        borderRadius: 2,
                        background: palette.fg,
                        opacity: op,
                        width: `${85 - i * 15}%`,
                      }}
                    />
                  ))}
                  {/* Accent button */}
                  <div
                    style={{
                      marginTop: 'auto',
                      height: 12,
                      borderRadius: 3,
                      background: palette.primary,
                      width: 44,
                      alignSelf: 'flex-end',
                    }}
                  />
                </div>
              </div>

              {/* Label */}
              <div
                style={{
                  padding: '5px 0',
                  fontSize: 11,
                  fontWeight: selected ? 700 : 500,
                  color: selected ? 'var(--primary)' : 'var(--fg-muted)',
                  textAlign: 'center',
                  background: 'var(--bg)',
                  borderTop: `1px solid var(--border)`,
                  letterSpacing: '0.03em',
                }}
              >
                {theme.label}
                {selected && (
                  <span style={{ marginLeft: 4, fontSize: 10, opacity: 0.7 }}>✓</span>
                )}
              </div>
            </button>
          )
        })}
      </div>
    </div>
  )
}
