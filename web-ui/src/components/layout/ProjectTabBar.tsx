import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { isProjectChildMode } from '@pando/client/services/api'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import type { ProjectTab } from '@pando/client/types'
import { isDesktop } from '@/services/desktop'
import { BrandMark } from '@/components/brand'
import { Badge, IconButton, Menu, MenuItem, Spinner, Tooltip } from '@/components/ui'
import { CircleStop, ExternalLink, Folder, RefreshCw, X } from '@/components/ui/icons'
import { SHELL_MAIN_PANEL_ID, type ProjectTabBarController } from './ProjectTabBarControls'
const LONG_PRESS_MS = 500

function stateLabel(t: (key: string, options?: Record<string, unknown>) => string, tab: ProjectTab): string {
  switch (tab.state) {
    case 'starting':
      return t('projects.tabs.starting')
    case 'running':
      return t('projects.tabs.stateRunning')
    case 'error':
      return t('projects.tabs.stateError')
    case 'stopped':
    default:
      return t('projects.tabs.stateStopped')
  }
}

function workspaceNameFromCwd(cwd: string | undefined, fallback: string): string {
  if (!cwd) return fallback
  const trimmed = cwd.replace(/[\\/]+$/, '')
  const segments = trimmed.split(/[\\/]/).filter(Boolean)
  return segments[segments.length - 1] || cwd || fallback
}

export default function ProjectTabBar({
  simple = false,
  controller,
}: {
  simple?: boolean
  controller: ProjectTabBarController
}) {
  const { t } = useTranslation()
  const tabs = useProjectTabsStore((state) => state.tabs)
  const activeTabId = useProjectTabsStore((state) => state.activeTabId)
  const workspace = useProjectStore((state) => state.workspace)
  const fetchWorkspace = useProjectStore((state) => state.fetchWorkspace)

  const scrollRef = useRef<HTMLDivElement>(null)
  const menuAnchorRef = useRef<HTMLElement | null>(null)
  const longPressTimerRef = useRef<number | null>(null)
  const skipClickRef = useRef<string | null>(null)

  const [overflowLeft, setOverflowLeft] = useState(false)
  const [overflowRight, setOverflowRight] = useState(false)
  const [menuProjectId, setMenuProjectId] = useState<string | null>(null)

  useEffect(() => {
    if (!workspace && tabs.length > 0 && !isProjectChildMode()) {
      void fetchWorkspace()
    }
  }, [fetchWorkspace, tabs.length, workspace])

  const visible = !isProjectChildMode() && tabs.length > 0 && (!simple || activeTabId !== 'main')

  const updateOverflow = useCallback(() => {
    const el = scrollRef.current
    if (!el) {
      setOverflowLeft(false)
      setOverflowRight(false)
      return
    }

    const maxScrollLeft = Math.max(el.scrollWidth - el.clientWidth, 0)
    setOverflowLeft(el.scrollLeft > 2)
    setOverflowRight(el.scrollLeft < maxScrollLeft - 2)
  }, [])

  useEffect(() => {
    if (!visible) return
    updateOverflow()
    const el = scrollRef.current
    if (!el) return

    el.addEventListener('scroll', updateOverflow, { passive: true })
    window.addEventListener('resize', updateOverflow)
    return () => {
      el.removeEventListener('scroll', updateOverflow)
      window.removeEventListener('resize', updateOverflow)
    }
  }, [tabs, updateOverflow, visible])

  const workspaceName = workspaceNameFromCwd(workspace?.cwd, t('projects.tabs.main'))
  const projectMap = useMemo(() => new Map(tabs.map((tab) => [tab.projectId, tab])), [tabs])
  const menuTab = menuProjectId ? projectMap.get(menuProjectId) ?? null : null

  const openMenu = useCallback((projectId: string, anchor: HTMLElement) => {
    menuAnchorRef.current = anchor
    setMenuProjectId(projectId)
  }, [])

  const closeMenu = useCallback(() => {
    setMenuProjectId(null)
  }, [])

  const cancelLongPress = useCallback(() => {
    if (longPressTimerRef.current !== null) {
      window.clearTimeout(longPressTimerRef.current)
      longPressTimerRef.current = null
    }
  }, [])

  useEffect(() => cancelLongPress, [cancelLongPress])

  const handleTabKeyDown = useCallback(
    (event: ReactKeyboardEvent<HTMLDivElement>, tabId: 'main' | string) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        controller.focusTab(tabId)
      }
    },
    [controller],
  )

  const handleTabClick = useCallback(
    (tabId: 'main' | string) => {
      if (skipClickRef.current === tabId) {
        skipClickRef.current = null
        return
      }
      controller.focusTab(tabId)
    },
    [controller],
  )

  const handleTabMouseDown = useCallback(
    (event: React.MouseEvent<HTMLDivElement>, projectId: string) => {
      if (event.button !== 1) return
      event.preventDefault()
      void controller.closeProjectTab(projectId)
    },
    [controller],
  )

  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>, projectId: string) => {
      cancelLongPress()
      if (event.pointerType !== 'touch' && event.pointerType !== 'pen') return

      const anchor = event.currentTarget
      longPressTimerRef.current = window.setTimeout(() => {
        skipClickRef.current = projectId
        openMenu(projectId, anchor)
      }, LONG_PRESS_MS)
    },
    [cancelLongPress, openMenu],
  )

  if (!visible) return null

  return (
    <div className="shell-projectbar">
      <div
        className="shell-projectbar-scrollwrap"
        data-overflow-left={overflowLeft || undefined}
        data-overflow-right={overflowRight || undefined}
      >
        <div
          ref={scrollRef}
          className="shell-projectbar-scroll"
          role="tablist"
          aria-label={t('projects.tabs.tablistLabel')}
        >
          <div
            role="tab"
            tabIndex={0}
            aria-selected={activeTabId === 'main'}
            aria-controls={SHELL_MAIN_PANEL_ID}
            aria-label={workspaceName}
            className="shell-projecttab shell-projecttab--home"
            onClick={() => handleTabClick('main')}
            onKeyDown={(event) => handleTabKeyDown(event, 'main')}
            title={workspace?.cwd || workspaceName}
          >
            <span className="shell-projecttab-icon shell-projecttab-icon--brand" aria-hidden="true">
              <BrandMark size={14} />
            </span>
            <span className="shell-projecttab-label">{workspaceName}</span>
          </div>

          {tabs.map((tab) => {
            const isActive = activeTabId === tab.projectId
            const isMenuOpen = menuProjectId === tab.projectId
            const delegations = tab.delegations ?? 0
            return (
              <div
                key={tab.projectId}
                role="tab"
                tabIndex={0}
                aria-selected={isActive}
                aria-controls={SHELL_MAIN_PANEL_ID}
                aria-label={tab.name}
                className="shell-projecttab"
                data-active={isActive || undefined}
                data-menu-open={isMenuOpen || undefined}
                onClick={() => handleTabClick(tab.projectId)}
                onKeyDown={(event) => handleTabKeyDown(event, tab.projectId)}
                onMouseDown={(event) => handleTabMouseDown(event, tab.projectId)}
                onContextMenu={(event) => {
                  event.preventDefault()
                  openMenu(tab.projectId, event.currentTarget)
                }}
                onPointerDown={(event) => handlePointerDown(event, tab.projectId)}
                onPointerLeave={cancelLongPress}
                onPointerCancel={cancelLongPress}
                onPointerUp={cancelLongPress}
                title={tab.path || tab.name}
              >
                <span className="shell-projecttab-icon" aria-hidden="true">
                  <Folder size={14} />
                </span>
                <span className="shell-projecttab-label">{tab.name}</span>
                <Tooltip content={stateLabel(t, tab)}>
                  <span className="shell-projecttab-state" aria-label={stateLabel(t, tab)}>
                    {tab.state === 'starting' ? (
                      <Spinner size={11} />
                    ) : (
                      <span
                        className="shell-projecttab-state-dot"
                        data-state={tab.state}
                        aria-hidden="true"
                      />
                    )}
                  </span>
                </Tooltip>
                {delegations > 0 && (
                  <Badge
                    tone="warning"
                    className="shell-projecttab-badge"
                    title={t('projects.tabs.delegations', { count: delegations })}
                  >
                    {delegations}
                  </Badge>
                )}
                <IconButton
                  aria-label={t('projects.tabs.closeCurrent')}
                  tooltip={t('projects.tabs.close')}
                  icon={<X size={12} />}
                  size="sm"
                  className="shell-projecttab-close"
                  onClick={(event) => {
                    event.stopPropagation()
                    void controller.closeProjectTab(tab.projectId)
                  }}
                />
              </div>
            )
          })}
        </div>
      </div>

      <Menu
        open={Boolean(menuTab && menuProjectId)}
        onClose={closeMenu}
        anchorRef={menuAnchorRef}
        placement="top-start"
        aria-label={t('projects.tabs.tablistLabel')}
      >
        <MenuItem
          icon={<X size={14} />}
          onSelect={() => {
            if (menuTab) {
              void controller.closeProjectTab(menuTab.projectId, 'keep')
            }
            closeMenu()
          }}
        >
          {t('projects.tabs.close')}
        </MenuItem>
        <MenuItem
          icon={<CircleStop size={14} />}
          danger
          onSelect={() => {
            if (menuTab) {
              void controller.closeProjectTab(menuTab.projectId, 'stop')
            }
            closeMenu()
          }}
        >
          {t('projects.tabs.closeAndStop')}
        </MenuItem>
        <MenuItem
          icon={<RefreshCw size={14} />}
          onSelect={() => {
            if (menuTab) {
              void controller.restartProjectTab(menuTab.projectId)
            }
            closeMenu()
          }}
        >
          {t('projects.tabs.restart')}
        </MenuItem>
        {isDesktop && menuTab && (
          <MenuItem
            icon={<ExternalLink size={14} />}
            onSelect={() => {
              void controller.openProjectInNewWindow(menuTab.projectId)
              closeMenu()
            }}
          >
            {t('projects.tabs.openInNewWindow')}
          </MenuItem>
        )}
        <MenuItem
          icon={<Folder size={14} />}
          onSelect={() => {
            controller.revealProject()
            closeMenu()
          }}
        >
          {t('projects.tabs.revealInProjects')}
        </MenuItem>
      </Menu>
    </div>
  )
}
