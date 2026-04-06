import { useEffect, useRef, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faUserTie, faChevronDown } from '@fortawesome/free-solid-svg-icons'
import api from '@/services/api'

function formatPersonaName(name: string): string {
  if (!name) return 'Auto'
  return name
    .split('-')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

export default function PersonaSelector() {
  const [personas, setPersonas] = useState<string[]>([])
  const [active, setActive] = useState<string>('')
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    api.get<{ personas: string[] }>('/api/v1/personas').then((d) => setPersonas(d.personas)).catch(() => {})
    api.get<{ active: string }>('/api/v1/personas/active').then((d) => setActive(d.active ?? '')).catch(() => {})
  }, [])

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    if (open) document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [open])

  async function selectPersona(name: string) {
    setOpen(false)
    if (loading) return
    setLoading(true)
    try {
      await api.put('/api/v1/personas/active', { name })
      setActive(name)
    } catch {
      // silently ignore
    } finally {
      setLoading(false)
    }
  }

  const options = ['', ...personas]

  return (
    <div ref={ref} style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
      <button
        onClick={() => setOpen((o) => !o)}
        title={`Persona: ${formatPersonaName(active)}`}
        style={{
          background: open ? 'var(--bg-secondary)' : 'none',
          border: '1px solid ' + (open ? 'var(--border)' : 'transparent'),
          cursor: 'pointer',
          color: active ? 'var(--primary)' : 'var(--fg-muted)',
          padding: '0.2rem 0.4rem',
          borderRadius: 'var(--radius-sm)',
          display: 'flex',
          alignItems: 'center',
          gap: '0.3rem',
          fontSize: 12,
          opacity: loading ? 0.6 : 1,
          transition: 'color 0.15s, background 0.15s, border-color 0.15s',
          whiteSpace: 'nowrap',
        }}
      >
        <FontAwesomeIcon icon={faUserTie} style={{ fontSize: 12 }} />
        <span className="persona-label">{formatPersonaName(active)}</span>
        <FontAwesomeIcon icon={faChevronDown} style={{ fontSize: 9, opacity: 0.6 }} />
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            right: 0,
            background: 'var(--sidebar-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            minWidth: 160,
            zIndex: 100,
            boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
            overflow: 'hidden',
          }}
        >
          {options.map((name) => {
            const isSelected = name === active
            return (
              <button
                key={name || '__auto__'}
                onClick={() => selectPersona(name)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.5rem',
                  width: '100%',
                  padding: '0.45rem 0.75rem',
                  background: isSelected ? 'var(--bg-secondary)' : 'none',
                  border: 'none',
                  cursor: 'pointer',
                  color: isSelected ? 'var(--primary)' : 'var(--fg-muted)',
                  fontSize: 13,
                  textAlign: 'left',
                  transition: 'background 0.1s, color 0.1s',
                }}
                onMouseEnter={(e) => {
                  if (!isSelected) {
                    ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--bg-secondary)'
                    ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)'
                  }
                }}
                onMouseLeave={(e) => {
                  if (!isSelected) {
                    ;(e.currentTarget as HTMLButtonElement).style.background = 'none'
                    ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted)'
                  }
                }}
              >
                <FontAwesomeIcon icon={faUserTie} style={{ fontSize: 11, opacity: isSelected ? 1 : 0.5 }} />
                {formatPersonaName(name)}
              </button>
            )
          })}
        </div>
      )}

      <style>{`
        @media (max-width: 768px) {
          .persona-label { display: none; }
        }
      `}</style>
    </div>
  )
}
