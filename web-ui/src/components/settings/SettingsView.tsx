import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useBlocker, useNavigate, type BlockerFunction } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import GeneralSettings from './GeneralSettings'
import AppearanceSettings from './AppearanceSettings'
import AgentsSettings from './AgentsSettings'
import MCPServersSettings from './MCPServersSettings'
import MCPGatewaySettings from './MCPGatewaySettings'
import LSPSettings from './LSPSettings'
import InternalToolsSettings from './InternalToolsSettings'
import BashSettings from './BashSettings'
import SandboxSettings from './SandboxSettings'
import TokenOptimizationSettings from './TokenOptimizationSettings'
import SkillsSettings from './SkillsSettings'
import LuaSettings from './LuaSettings'
import EvaluatorSettings from './EvaluatorSettings'
import MesnadaSettings from './MesnadaSettings'
import RemembrancesSettings from './RemembrancesSettings'
import SnapshotsSettings from './SnapshotsSettings'
import DesignSystemSettings from './DesignSystemSettings'
import APIServerSettings from './APIServerSettings'
import WebUIAccessSettings from './WebUIAccessSettings'
import ProviderAccountsSettings from './ProviderAccountsSettings'
import ContainerRuntimeSettings from './ContainerRuntimeSettings'
import UnsavedChangesDialog from './UnsavedChangesDialog'
import { useUnsavedChangesStore } from './unsavedChanges'
import { useConfigEventsStore } from '@pando/client/stores/configEventsStore'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { useUIPolicyStore } from '@pando/client/stores/uiPolicyStore'
import ExtensionSlot from '@/components/extensions/ExtensionSlot'
import { Button } from '@/components/ui'
import {
  ArrowLeft,
  Bookmark,
  Bot,
  Boxes,
  Brain,
  Code,
  FileCode,
  Gauge,
  Globe,
  History,
  LayoutGrid,
  Lock,
  Network,
  Palette,
  Plug,
  Puzzle,
  Server,
  Settings2,
  Shield,
  Sparkles,
  Terminal,
  Workflow,
  Wrench,
  type LucideIcon,
} from '@/components/ui/icons'

type SettingsCategory =
  | 'general'
  | 'appearance'
  | 'providers'
  | 'agents'
  | 'mcp-servers'
  | 'mcp-gateway'
  | 'lsp'
  | 'tools'
  | 'sandbox'
  | 'bash'
  | 'token-optimization'
  | 'skills'
  | 'design-system'
  | 'lua'
  | 'self-improvement'
  | 'mesnada'
  | 'remembrances'
  | 'snapshots'
  | 'api-server'
  | 'webui-access'
  | 'container-runtime'

/**
 * The settings categories, each with the configuration path it edits.
 *
 * The path is what the UI policy (GET /api/v1/config/ui-policy) matches when an
 * extension declares a section hidden. A category with no path edits nothing an
 * extension can own and is always shown.
 */
const CATEGORY_KEYS: { id: SettingsCategory; labelKey: string; icon: LucideIcon; group?: string; path?: string }[] = [
  { id: 'general', labelKey: 'settings.categories.general', icon: Settings2 },
  { id: 'appearance', labelKey: 'settings.categories.appearance', icon: Palette },
  { id: 'providers', labelKey: 'settings.categories.providers', icon: Plug, path: 'providerAccounts' },
  { id: 'agents', labelKey: 'settings.categories.agents', icon: Bot, path: 'agents' },
  { id: 'mcp-servers', labelKey: 'settings.categories.mcpServers', icon: Server, path: 'mcpServers' },
  { id: 'mcp-gateway', labelKey: 'settings.categories.mcpGateway', icon: Network, path: 'mcpGateway' },
  { id: 'lsp', labelKey: 'settings.categories.lsp', icon: Code, path: 'lsp' },
  { id: 'tools', labelKey: 'settings.categories.tools', icon: Wrench, path: 'internalTools' },
  { id: 'container-runtime', labelKey: 'settings.categories.containerRuntime', icon: Boxes, path: 'container' },
  { id: 'sandbox', labelKey: 'settings.categories.sandbox', icon: Shield, path: 'sandbox' },
  { id: 'bash', labelKey: 'settings.categories.bash', icon: Terminal, path: 'bash' },
  { id: 'token-optimization', labelKey: 'settings.categories.tokenOptimization', icon: Gauge, path: 'tokenOptimization' },
  { id: 'skills', labelKey: 'settings.categories.skills', icon: Sparkles, path: 'skills' },
  { id: 'design-system', labelKey: 'settings.categories.designSystem', icon: LayoutGrid },
  { id: 'lua', labelKey: 'settings.categories.lua', icon: FileCode, path: 'lua' },
  { id: 'self-improvement', labelKey: 'settings.categories.selfImprovement', icon: Brain, path: 'evaluator' },
  { id: 'mesnada', labelKey: 'settings.categories.mesnada', icon: Workflow, group: 'services', path: 'mesnada' },
  { id: 'remembrances', labelKey: 'settings.categories.remembrances', icon: Bookmark, group: 'services', path: 'remembrances' },
  { id: 'snapshots', labelKey: 'settings.categories.snapshots', icon: History, group: 'services', path: 'snapshots' },
  { id: 'api-server', labelKey: 'settings.categories.apiServer', icon: Globe, group: 'services', path: 'server' },
  { id: 'webui-access', labelKey: 'settings.categories.webuiAccess', icon: Lock, group: 'services' },
]

const PANELS: Partial<Record<SettingsCategory, ReactNode>> = {
  general: <GeneralSettings />,
  appearance: <AppearanceSettings />,
  providers: <ProviderAccountsSettings />,
  agents: <AgentsSettings />,
  'mcp-servers': <MCPServersSettings />,
  'mcp-gateway': <MCPGatewaySettings />,
  lsp: <LSPSettings />,
  tools: <InternalToolsSettings />,
  'container-runtime': <ContainerRuntimeSettings />,
  sandbox: <SandboxSettings />,
  bash: <BashSettings />,
  'token-optimization': <TokenOptimizationSettings />,
  skills: <SkillsSettings />,
  lua: <LuaSettings />,
  'self-improvement': <EvaluatorSettings />,
  mesnada: <MesnadaSettings />,
  remembrances: <RemembrancesSettings />,
  snapshots: <SnapshotsSettings />,
  'design-system': <DesignSystemSettings />,
  'api-server': <APIServerSettings />,
  'webui-access': <WebUIAccessSettings />,
}

/**
 * A settings section contributed by an extension, addressed by its panel ID.
 * Prefixing keeps the extension namespace and the core one from ever colliding.
 */
type ExtensionCategory = `ext:${string}`
type ActiveCategory = SettingsCategory | ExtensionCategory

/** The route Esc leaves Settings for: the same target as the sidebar "Chat" item. */
const CHAT_ROUTE = '/'

/**
 * Anything open that owns Esc (dialogs, popovers, menus, listboxes, the command
 * overlays, the mobile drawer). Their own handlers close them; Settings must
 * not also treat that Esc as "leave".
 */
const ESC_OWNER_SELECTOR = [
  '.ui-dialog-overlay',
  '.ui-popover',
  '.ovl-scrim',
  '.shell-scrim',
  '[aria-modal="true"]',
  '[role="menu"]',
  '[role="listbox"]',
].join(',')

/** Leaving the settings route while a panel holds unsaved edits is blocked. */
const blockWhenUnsaved: BlockerFunction = ({ currentLocation, nextLocation }) =>
  currentLocation.pathname !== nextLocation.pathname && useUnsavedChangesStore.getState().hasUnsaved()

/** Below this width the view switches to a master/detail flow. */
const MOBILE_QUERY = '(max-width: 768px)'

export default function SettingsView() {
  const { t } = useTranslation()
  const [activeCategory, setActiveCategory] = useState<ActiveCategory>('general')
  const { connect, disconnect } = useConfigEventsStore()
  const extensionSections = useExtensionPanelsStore((s) => s.panels).filter((p) => p.slot === 'settings')

  // The settings policy an extension may declare. It can change while the app
  // runs, so it is fetched whenever this view mounts rather than once at boot.
  const loadUIPolicy = useUIPolicyStore((s) => s.load)
  const uiPolicy = useUIPolicyStore((s) => s.policy)
  const isSectionHidden = useUIPolicyStore((s) => s.isHidden)
  useEffect(() => { void loadUIPolicy() }, [loadUIPolicy])

  // A hidden category is not offered at all. Hiding is presentation: the
  // backend refuses writes to the same paths, so this only decides what the
  // user is asked to fill in, never what may be changed.
  const categories = CATEGORY_KEYS.filter((c) => !c.path || !isSectionHidden(c.path))

  // A policy that arrives while a now-hidden category is open falls back to the
  // first one still offered, so the view never renders a section it just
  // stopped listing.
  const visibleCategory: ActiveCategory =
    activeCategory.startsWith('ext:') || categories.some((c) => c.id === activeCategory)
      ? activeCategory
      : (categories[0]?.id ?? 'general')

  // Track the mobile breakpoint so the category list and the section can take
  // turns owning the full width instead of splitting it.
  const [isMobile, setIsMobile] = useState(() => window.matchMedia(MOBILE_QUERY).matches)
  useEffect(() => {
    const mql = window.matchMedia(MOBILE_QUERY)
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  // Mobile only: the category list is the landing screen; picking a category
  // swaps it for the section, which offers a control to bring the list back.
  const [menuOpen, setMenuOpen] = useState(true)
  const showMenu = !isMobile || menuOpen
  const showContent = !isMobile || !menuOpen

  // ---- Unsaved-changes guard -------------------------------------------
  // In-view transitions (category switch, mobile back) are deferred through
  // `pendingAction`; route changes (sidebar, Esc, browser back) are held by
  // the router blocker. Either way the same dialog decides what happens.
  const navigate = useNavigate()
  const blocker = useBlocker(blockWhenUnsaved)
  const [pendingAction, setPendingAction] = useState<(() => void) | null>(null)
  const dirtyEntries = useUnsavedChangesStore((s) => s.entries)
  const dialogOpen = pendingAction !== null || blocker.state === 'blocked'

  const guarded = useCallback((action: () => void) => {
    if (useUnsavedChangesStore.getState().hasUnsaved()) setPendingAction(() => action)
    else action()
  }, [])

  const continuePending = () => {
    const action = pendingAction
    setPendingAction(null)
    if (blocker.state === 'blocked') blocker.proceed()
    action?.()
  }

  const cancelPending = () => {
    setPendingAction(null)
    if (blocker.state === 'blocked') blocker.reset()
  }

  const saveAndContinue = async () => {
    const ok = await useUnsavedChangesStore.getState().saveAll()
    if (ok) continuePending()
    return ok
  }

  const discardAndContinue = () => {
    useUnsavedChangesStore.getState().discardAll()
    continuePending()
  }

  const categoryLabel = (id: string) => {
    const core = CATEGORY_KEYS.find((c) => c.id === id)
    if (core) return t(core.labelKey)
    const ext = id.startsWith('ext:') ? extensionSections.find((p) => `ext:${p.id}` === id) : undefined
    return ext ? ext.title || ext.id : id
  }
  const unsavedSections = Object.values(dirtyEntries)
    .filter((e) => e.dirty)
    .map((e) => e.label ?? categoryLabel(e.id))

  // Esc leaves Settings for the chat, unless something open owns the key.
  // Navigating goes through the blocker, so unsaved edits still prompt first.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || e.isComposing) return
      if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return
      if (document.querySelector(ESC_OWNER_SELECTOR)) return
      const active = document.activeElement
      if (active instanceof HTMLElement && active.getAttribute('aria-expanded') === 'true') return
      navigate(CHAT_ROUTE)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])

  // A reload or tab close with unsaved edits asks the browser to confirm.
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!useUnsavedChangesStore.getState().hasUnsaved()) return
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [])

  const selectCategory = (id: ActiveCategory) => {
    if (id === activeCategory) {
      setMenuOpen(false)
      return
    }
    guarded(() => {
      setActiveCategory(id)
      setMenuOpen(false)
    })
  }

  // Connect to the config hot-reload SSE stream while this view is mounted.
  useEffect(() => {
    connect()
    return () => { disconnect() }
  }, [connect, disconnect])

  const activePanel = visibleCategory.startsWith('ext:') ? undefined : PANELS[visibleCategory as SettingsCategory]

  const renderNavButton = (cat: (typeof CATEGORY_KEYS)[number]) => {
    const Icon = cat.icon
    const isActive = activeCategory === cat.id
    return (
      <button
        key={cat.id}
        type="button"
        className="settings-nav-item"
        aria-current={isActive || undefined}
        onClick={() => selectCategory(cat.id)}
      >
        <Icon size={16} aria-hidden />
        <span className="settings-nav-item-label">{t(cat.labelKey)}</span>
      </button>
    )
  }

  return (
    <div className="settings-shell">
      {/* Category list — mini sidebar on desktop, full-width menu on mobile */}
      {showMenu && (
        <nav className={isMobile ? 'settings-nav settings-nav--mobile' : 'settings-nav'} aria-label={t('settings.title', 'Settings')}>
          <div className="settings-nav-group">
            {categories.filter((c) => !c.group).map(renderNavButton)}
          </div>

          {categories.some((c) => c.group === 'services') && (
            <div className="settings-nav-group">
              <div className="settings-nav-group-label">{t('nav.sections.services')}</div>
              {categories.filter((c) => c.group === 'services').map(renderNavButton)}
            </div>
          )}

          {extensionSections.length > 0 && (
            <div className="settings-nav-group">
              <div className="settings-nav-group-label">{t('settings.categories.extensions', 'Extensions')}</div>
              {extensionSections.map((panel) => {
                const id: ExtensionCategory = `ext:${panel.id}`
                const isActive = activeCategory === id
                return (
                  <button
                    key={panel.id}
                    type="button"
                    className="settings-nav-item"
                    aria-current={isActive || undefined}
                    onClick={() => selectCategory(id)}
                  >
                    <Puzzle size={16} aria-hidden />
                    <span className="settings-nav-item-label">{panel.title || panel.id}</span>
                  </button>
                )
              })}
            </div>
          )}
        </nav>
      )}

      {/* Content area */}
      {showContent && (
        <div className="settings-content">
          <div className="settings-content-inner">
            {isMobile && (
              <Button
                className="settings-mobile-back"
                variant="ghost"
                size="sm"
                icon={<ArrowLeft size={14} />}
                onClick={() => guarded(() => setMenuOpen(true))}
              >
                {t('settings.backToCategories')}
              </Button>
            )}

            {(uiPolicy.banner.text || uiPolicy.banner.link) && (
              <div className="settings-banner">
                <span>
                  {uiPolicy.banner.text}
                  {uiPolicy.banner.link && (
                    <>
                      {' '}
                      <a href={uiPolicy.banner.link} target="_blank" rel="noreferrer">
                        {uiPolicy.banner.link}
                      </a>
                    </>
                  )}
                </span>
              </div>
            )}

            {activePanel}
            {activeCategory.startsWith('ext:') && (
              <ExtensionSlot slot="settings" panelId={activeCategory.slice('ext:'.length)} />
            )}
          </div>
        </div>
      )}

      <UnsavedChangesDialog
        open={dialogOpen}
        sections={unsavedSections}
        onSave={saveAndContinue}
        onDiscard={discardAndContinue}
        onCancel={cancelPending}
      />
    </div>
  )
}
