import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

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

/**
 * Menu-styled list of slash commands floating above the composer. Keyboard
 * navigation stays in the textarea (ChatInput drives `selectedIndex`), so this
 * is a listbox that never takes focus rather than a focus-trapping Menu.
 */
export default function SlashCommandMenu({
  commands,
  filter,
  selectedIndex,
  onSelect,
  visible,
}: SlashCommandMenuProps) {
  const { t } = useTranslation()
  const selectedRef = useRef<HTMLDivElement>(null)

  // Scroll selected item into view
  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest' })
  }, [selectedIndex])

  if (!visible || commands.length === 0) return null

  const filtered = commands.filter((cmd) =>
    cmd.name.toLowerCase().startsWith(filter.toLowerCase()),
  )

  if (filtered.length === 0) return null

  return (
    <div className="chat-slash" role="listbox" aria-label={t('chat.slashCommands')}>
      <div className="chat-slash-label">{t('chat.slashCommands')}</div>
      {filtered.map((cmd, idx) => {
        const isSelected = idx === selectedIndex
        return (
          <div
            key={cmd.name}
            ref={isSelected ? selectedRef : undefined}
            role="option"
            aria-selected={isSelected}
            className="chat-slash-item"
            // Keep focus in the textarea while picking with the mouse.
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onSelect(cmd)}
          >
            <span className="chat-slash-name">/{cmd.name}</span>
            <span className="chat-slash-desc">{cmd.description}</span>
          </div>
        )
      })}
    </div>
  )
}
