import { useEffect, useRef, useState } from 'react'
import { UserRound, ChevronDown } from '@/components/ui/icons'
import { Menu, MenuItem } from '@/components/ui'
import api from '@pando/client/services/api'

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
  const buttonRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    api.get<{ personas: string[] }>('/api/v1/personas').then((d) => setPersonas(d.personas)).catch(() => {})
    api.get<{ active: string }>('/api/v1/personas/active').then((d) => setActive(d.active ?? '')).catch(() => {})
  }, [])

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
    <div className="flex items-center">
      <button
        ref={buttonRef}
        onClick={() => setOpen((o) => !o)}
        title={`Persona: ${formatPersonaName(active)}`}
        data-active={!!active || undefined}
        data-open={open || undefined}
        className={`persona-trigger${loading ? ' opacity-60' : ''}`}
      >
        <UserRound size={14} />
        <span className="persona-label">{formatPersonaName(active)}</span>
        <ChevronDown size={14} className="opacity-60" />
      </button>

      <Menu open={open} onClose={() => setOpen(false)} anchorRef={buttonRef} placement="bottom-end" aria-label="Select persona">
        {options.map((name) => (
          <MenuItem
            key={name || '__auto__'}
            icon={<UserRound size={14} />}
            checked={name === active}
            onSelect={() => void selectPersona(name)}
          >
            {formatPersonaName(name)}
          </MenuItem>
        ))}
      </Menu>
    </div>
  )
}
