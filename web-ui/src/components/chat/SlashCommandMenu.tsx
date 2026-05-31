import { useEffect, useRef } from 'react'

export interface SlashCommandItem {
  name: string
  description: string
  acceptsArgs: boolean
}

interface SlashCommandMenuProps {
  commands: SlashCommandItem[]
  filter: string
  selectedIndex: number
  onSelect: (cmd: SlashCommandItem) => void
  visible: boolean
}

export default function SlashCommandMenu({
  commands,
  filter,
  selectedIndex,
  onSelect,
  visible,
}: SlashCommandMenuProps) {
  const listRef = useRef<HTMLDivElement>(null)
  const selectedRef = useRef<HTMLDivElement>(null)

  // Scroll selected item into view
  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest' })
  }, [selectedIndex])

  if (!visible || commands.length === 0) return null

  const filtered = commands.filter((cmd) =>
    cmd.name.toLowerCase().startsWith(filter.toLowerCase())
  )

  if (filtered.length === 0) return null

  return (
    <div
      ref={listRef}
      style={{
        position: 'absolute',
        bottom: '100%',
        left: 0,
        right: 0,
        maxHeight: 220,
        overflowY: 'auto',
        background: 'var(--panel, var(--bg))',
        border: '1px solid var(--border)',
        borderBottom: 'none',
        zIndex: 100,
        fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
        fontSize: 13,
      }}
    >
      {filtered.map((cmd, idx) => {
        const isSelected = idx === selectedIndex
        return (
          <div
            key={cmd.name}
            ref={isSelected ? selectedRef : undefined}
            onClick={() => onSelect(cmd)}
            style={{
              padding: '6px 12px',
              cursor: 'pointer',
              background: isSelected ? 'var(--primary-bg, rgba(var(--primary-rgb, 100,100,255), 0.15))' : 'transparent',
              color: isSelected ? 'var(--primary)' : 'var(--fg)',
              display: 'flex',
              gap: 8,
              alignItems: 'baseline',
            }}
          >
            <span style={{ fontWeight: 600 }}>/{cmd.name}</span>
            <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
              {cmd.description}
            </span>
          </div>
        )
      })}
    </div>
  )
}
