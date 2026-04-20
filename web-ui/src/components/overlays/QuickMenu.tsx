import { useEffect, useRef, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faMagnifyingGlass,
  faComments,
  faCog,
  faFileLines,
  faNetworkWired,
  faCamera,
  faStar,
  faCode,
  faTerminal,
  faMoon,
  faRotateRight,
  faFile,
} from '@fortawesome/free-solid-svg-icons'
import type { IconDefinition } from '@fortawesome/fontawesome-svg-core'
import { useLayoutStore } from '@/stores/layoutStore'
import { useTheme } from '@/hooks/useTheme'

const RECENT_KEY = 'pando-quick-menu-recent'

interface MenuItem {
  id: string
  label: string
  icon: IconDefinition
  group: 'view' | 'command' | 'recent'
  path?: string
  action?: () => void
  description?: string
}

const VIEWS: Omit<MenuItem, 'group'>[] = [
  { id: 'chat', label: 'Chat', icon: faComments, path: '/', description: '/' },
  { id: 'settings', label: 'Settings', icon: faCog, path: '/settings', description: '/settings' },
  { id: 'logs', label: 'Logs', icon: faFileLines, path: '/logs', description: '/logs' },
  { id: 'orchestrator', label: 'Orchestrator', icon: faNetworkWired, path: '/orchestrator', description: '/orchestrator' },
  { id: 'snapshots', label: 'Snapshots', icon: faCamera, path: '/snapshots', description: '/snapshots' },
  { id: 'evaluator', label: 'Self-Improvement', icon: faStar, path: '/evaluator', description: '/evaluator' },
  { id: 'editor', label: 'Code Editor', icon: faCode, path: '/editor', description: '/editor' },
  { id: 'terminal', label: 'Terminal', icon: faTerminal, path: '/terminal', description: '/terminal' },
]

function loadRecent(): string[] {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]')
  } catch {
    return []
  }
}

function saveRecent(ids: string[]) {
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(ids.slice(0, 5)))
  } catch {
    // ignore
  }
}

function addToRecent(id: string) {
  const prev = loadRecent().filter((x) => x !== id)
  saveRecent([id, ...prev])
}

export default function QuickMenu() {
  const navigate = useNavigate()
  const { setQuickMenuOpen } = useLayoutStore()
  const { toggleTheme } = useTheme()
  const [query, setQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const [recentIds, setRecentIds] = useState<string[]>(loadRecent)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const close = useCallback(() => {
    setQuickMenuOpen(false)
  }, [setQuickMenuOpen])

  // Build all items
  const allItems: MenuItem[] = [
    ...VIEWS.map((v) => ({ ...v, group: 'view' as const })),
    {
      id: 'web-ui-settings',
      label: 'Web UI Settings',
      icon: faCog,
      group: 'command',
      path: '/settings',
      description: 'Web UI Settings',
    },
    {
      id: 'toggle-dark-mode',
      label: 'Toggle Dark Mode',
      icon: faMoon,
      group: 'command',
      action: () => { toggleTheme(); close() },
    },
    {
      id: 'reload',
      label: 'Reload',
      icon: faRotateRight,
      group: 'command',
      action: () => { window.location.reload() },
    },
  ]

  // Filter by query
  const q = query.toLowerCase()
  const filtered = allItems.filter(
    (item) =>
      !q ||
      item.label.toLowerCase().includes(q) ||
      (item.description ?? '').toLowerCase().includes(q),
  )

  // Build recent items from ids
  const recentItems: MenuItem[] = recentIds
    .map((id) => allItems.find((x) => x.id === id))
    .filter((x): x is MenuItem => Boolean(x))
    .map((x) => ({ ...x, group: 'recent' as const }))

  // Groups to show
  type Group = { label: string; items: MenuItem[] }
  const groups: Group[] = []

  if (!q && recentItems.length > 0) {
    groups.push({ label: 'Recently Used', items: recentItems })
  }

  const viewItems = filtered.filter((x) => x.group === 'view')
  const commandItems = filtered.filter((x) => x.group === 'command')

  if (viewItems.length > 0) groups.push({ label: 'Views', items: viewItems })
  if (commandItems.length > 0) groups.push({ label: 'Commands', items: commandItems })

  // Flat list for keyboard nav
  const flatItems = groups.flatMap((g) => g.items)

  const normalizedSelectedIndex = query ? 0 : selectedIndex

  const execute = useCallback(
    (item: MenuItem) => {
      addToRecent(item.id)
      setRecentIds(loadRecent())
      if (item.action) {
        item.action()
      } else if (item.path) {
        navigate(item.path)
        close()
      }
    },
    [navigate, close],
  )

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        close()
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex((i) => Math.min(i + 1, flatItems.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex((i) => Math.max(i - 1, 0))
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        const item = flatItems[normalizedSelectedIndex]
        if (item) execute(item)
        return
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [flatItems, normalizedSelectedIndex, execute, close])

  // Scroll selected item into view
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-selected="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [normalizedSelectedIndex])

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'center',
        paddingTop: '10vh',
        animation: 'qm-fade-in 0.15s ease',
      }}
      onClick={close}
    >
      <style>{`
        @keyframes qm-fade-in {
          from { opacity: 0; }
          to { opacity: 1; }
        }
        @keyframes qm-slide-up {
          from { opacity: 0; transform: translateY(12px) scale(0.98); }
          to { opacity: 1; transform: translateY(0) scale(1); }
        }
      `}</style>
      <div
        style={{
          width: 620,
          maxWidth: 'calc(100vw - 2rem)',
          maxHeight: 500,
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          boxShadow: '0 16px 48px rgba(0,0,0,0.24)',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          animation: 'qm-slide-up 0.15s ease',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search input */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.75rem',
            padding: '0.875rem 1rem',
            borderBottom: '1px solid var(--border)',
          }}
        >
          <FontAwesomeIcon
            icon={faMagnifyingGlass}
            style={{ color: 'var(--fg-dim)', fontSize: 14, flexShrink: 0 }}
          />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search files and commands..."
            style={{
              flex: 1,
              background: 'none',
              border: 'none',
              outline: 'none',
              fontSize: 14,
              color: 'var(--fg)',
            }}
          />
          <kbd
            style={{
              fontSize: 11,
              color: 'var(--fg-dim)',
              background: 'var(--surface)',
              border: '1px solid var(--border)',
              borderRadius: 0,
              padding: '1px 5px',
            }}
          >
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div ref={listRef} style={{ flex: 1, overflowY: 'auto', padding: '0.25rem 0' }}>
          {groups.length === 0 ? (
            <div
              style={{
                padding: '2rem',
                textAlign: 'center',
                fontSize: 13,
                color: 'var(--fg-muted)',
              }}
            >
              No results for &ldquo;{query}&rdquo;
            </div>
          ) : (
            groups.map((group) => {
              let flatOffset = 0
              for (const g of groups) {
                if (g.label === group.label) break
                flatOffset += g.items.length
              }
              return (
                <div key={group.label}>
                  <div
                    style={{
                      padding: '0.5rem 1rem 0.25rem',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--fg-dim)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.06em',
                    }}
                  >
                    {group.label}
                  </div>
                  {group.items.map((item, idx) => {
                    const isSelected = normalizedSelectedIndex === flatOffset + idx
                    return (
                      <div
                        key={`${group.label}-${item.id}`}
                        data-selected={isSelected ? 'true' : undefined}
                        onClick={() => execute(item)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: '0.75rem',
                          padding: '0.5rem 1rem',
                          cursor: 'pointer',
                          background: isSelected ? 'var(--selected)' : 'transparent',
                          borderRadius: 'var(--radius-sm)',
                          margin: '1px 0.5rem',
                          transition: 'background 0.1s',
                        }}
                        onMouseEnter={() => setSelectedIndex(flatOffset + idx)}
                      >
                        <FontAwesomeIcon
                          icon={group.label === 'Recently Used' ? faFile : item.icon}
                          style={{
                            fontSize: 13,
                            color: isSelected ? 'var(--primary)' : 'var(--fg-muted)',
                            width: 16,
                            flexShrink: 0,
                          }}
                        />
                        <span
                          style={{
                            flex: 1,
                            fontSize: 13,
                            color: 'var(--fg)',
                            fontWeight: isSelected ? 500 : 400,
                          }}
                        >
                          {item.label}
                        </span>
                        {item.description && (
                          <span style={{ fontSize: 11, color: 'var(--fg-dim)' }}>
                            {item.description}
                          </span>
                        )}
                      </div>
                    )
                  })}
                </div>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}
