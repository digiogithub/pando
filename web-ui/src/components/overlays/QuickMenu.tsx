import { useEffect, useRef, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useTheme } from '@/hooks/useTheme'
import { useToast } from '@pando/client/stores/toastStore'
import { loadLauncherCommands, type LauncherCommand } from '@pando/client/services/commandLauncher'
import { Kbd } from '@/components/ui'
import {
  Search,
  MessageSquare,
  Settings,
  FileText,
  Network,
  GitBranch,
  Star,
  Code,
  SquareTerminal,
  Moon,
  RefreshCw,
  FileIcon,
  LogIn,
  Key,
  Info,
  LogOut,
  ChartColumn,
  type LucideIcon,
} from '@/components/ui/icons'
import '@/styles/overlays.css'

const RECENT_KEY = 'pando-quick-menu-recent'

interface MenuItem {
  id: string
  label: string
  icon: LucideIcon
  group: 'view' | 'command' | 'recent' | 'account'
  path?: string
  action?: () => void | Promise<void>
  description?: string
  keywords?: string[]
}

const VIEWS: Omit<MenuItem, 'group'>[] = [
  { id: 'chat', label: 'Chat', icon: MessageSquare, path: '/', description: '/' },
  { id: 'settings', label: 'Settings', icon: Settings, path: '/settings', description: '/settings' },
  { id: 'logs', label: 'Logs', icon: FileText, path: '/logs', description: '/logs' },
  { id: 'orchestrator', label: 'Orchestrator', icon: Network, path: '/orchestrator', description: '/orchestrator' },
  { id: 'snapshots', label: 'Agent VCS', icon: GitBranch, path: '/snapshots', description: '/snapshots — version control' },
  { id: 'evaluator', label: 'Self-Improvement', icon: Star, path: '/evaluator', description: '/evaluator' },
  { id: 'editor', label: 'Code Editor', icon: Code, path: '/editor', description: '/editor' },
  { id: 'terminal', label: 'Terminal', icon: SquareTerminal, path: '/terminal', description: '/terminal' },
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

const AUTH_ICONS: Record<string, LucideIcon> = {
  'anthropic:login': LogIn,
  'anthropic:complete-login': Key,
  'anthropic:status': Info,
  'anthropic:logout': LogOut,
  'anthropic:usage': ChartColumn,
  'copilot:login': LogIn,
  'copilot:status': Info,
  'copilot:logout': LogOut,
}

function iconForCommand(command: LauncherCommand): LucideIcon {
  return AUTH_ICONS[command.id] ?? Info
}

export default function QuickMenu() {
  const navigate = useNavigate()
  const { setQuickMenuOpen } = useLayoutStore()
  const { toggleMode: toggleTheme } = useTheme()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const [recentIds, setRecentIds] = useState<string[]>(loadRecent)
  const [accountCommands, setAccountCommands] = useState<MenuItem[]>([])

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const close = useCallback(() => {
    setQuickMenuOpen(false)
  }, [setQuickMenuOpen])

  useEffect(() => {
    let cancelled = false
    void loadLauncherCommands((message, type = 'info') => {
      if (type === 'success') toast.success(message, 6000)
      else if (type === 'error') toast.error(message, 7000)
      else if (type === 'warning') toast.warning(message, 7000)
      else toast.info(message, 8000)
    })
      .then((commands) => {
        if (cancelled) return
        setAccountCommands(commands.map((command) => ({
          id: command.id,
          label: command.label,
          icon: iconForCommand(command),
          group: 'account' as const,
          description: command.description,
          keywords: command.keywords,
          action: command.action,
        })))
      })
      .catch((error) => {
        if (cancelled) return
        toast.error(error instanceof Error ? error.message : 'Failed to load launcher commands')
      })
    return () => {
      cancelled = true
    }
  }, [toast])

  // Build all items
  const allItems: MenuItem[] = [
    ...VIEWS.map((v) => ({ ...v, group: 'view' as const })),
    {
      id: 'web-ui-settings',
      label: 'Web UI Settings',
      icon: Settings,
      group: 'command',
      path: '/settings',
      description: 'Web UI Settings',
    },
    {
      id: 'toggle-dark-mode',
      label: 'Toggle Dark Mode',
      icon: Moon,
      group: 'command',
      action: () => { toggleTheme(); close() },
    },
    {
      id: 'reload',
      label: 'Reload',
      icon: RefreshCw,
      group: 'command',
      action: () => { window.location.reload() },
    },
    ...accountCommands,
  ]

  // Filter by query
  const q = query.toLowerCase()
  const filtered = allItems.filter(
    (item) =>
      !q ||
      item.label.toLowerCase().includes(q) ||
      (item.description ?? '').toLowerCase().includes(q) ||
      item.keywords?.some((keyword) => keyword.toLowerCase().includes(q)),
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
  const accountItems = filtered.filter((x) => x.group === 'account')

  if (viewItems.length > 0) groups.push({ label: 'Views', items: viewItems })
  if (commandItems.length > 0) groups.push({ label: 'Commands', items: commandItems })
  if (accountItems.length > 0) groups.push({ label: 'Accounts', items: accountItems })

  // Flat list for keyboard nav
  const flatItems = groups.flatMap((g) => g.items)

  const normalizedSelectedIndex = query ? 0 : selectedIndex

  const execute = useCallback(
    async (item: MenuItem) => {
      addToRecent(item.id)
      setRecentIds(loadRecent())
      if (item.action) {
        await item.action()
        close()
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
        if (item) {
          void execute(item)
        }
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
    <div className="ovl-scrim" onClick={close}>
      <div className="ovl-panel" onClick={(e) => e.stopPropagation()}>
        {/* Search input */}
        <div className="ovl-search">
          <Search size={14} />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search files and commands..."
            className="ovl-search-input"
          />
          <Kbd>ESC</Kbd>
        </div>

        {/* Results */}
        <div ref={listRef} className="ovl-list">
          {groups.length === 0 ? (
            <div className="ovl-empty">No results for &ldquo;{query}&rdquo;</div>
          ) : (
            groups.map((group) => {
              let flatOffset = 0
              for (const g of groups) {
                if (g.label === group.label) break
                flatOffset += g.items.length
              }
              return (
                <div key={group.label}>
                  <div className="ovl-group-label">{group.label}</div>
                  {group.items.map((item, idx) => {
                    const isSelected = normalizedSelectedIndex === flatOffset + idx
                    const Icon = group.label === 'Recently Used' ? FileIcon : item.icon
                    return (
                      <div
                        key={`${group.label}-${item.id}`}
                        data-selected={isSelected ? 'true' : undefined}
                        onClick={() => { void execute(item) }}
                        onMouseEnter={() => setSelectedIndex(flatOffset + idx)}
                        className="ovl-item"
                      >
                        <span className="ovl-item-icon"><Icon size={14} /></span>
                        <span className="ovl-item-label">{item.label}</span>
                        {item.description && <span className="ovl-item-desc">{item.description}</span>}
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
