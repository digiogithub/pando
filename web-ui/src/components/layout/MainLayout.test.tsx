import '@/i18n'
import '@testing-library/jest-dom/vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import MainLayout from './MainLayout'

vi.mock('@pando/client/services/auth', () => ({
  authenticate: vi.fn().mockResolvedValue('token'),
}))

vi.mock('@/services/desktopWindow', () => ({
  useProvidesWindowTitleBar: vi.fn(),
}))

vi.mock('./shellHooks', () => ({
  MOBILE_QUERY: '(max-width: 768px)',
  needsMacTrafficLightInset: false,
  readSidebarPref: vi.fn().mockReturnValue(true),
  useMediaQuery: vi.fn().mockReturnValue(false),
  writeSidebarPref: vi.fn(),
}))

vi.mock('@/components/overlays/QuickMenu', () => ({ default: () => <div data-testid="quick-menu" /> }))
vi.mock('@/components/overlays/ModelSwitcher', () => ({ default: () => <div data-testid="model-switcher" /> }))
vi.mock('@/components/setup/SetupWizard', () => ({ default: () => <div data-testid="setup-wizard" /> }))
vi.mock('@/components/chat/PermissionDialog', () => ({ default: () => <div data-testid="permission-dialog" /> }))
vi.mock('@/components/chat/QuestionDialog', () => ({ default: () => <div data-testid="question-dialog" /> }))
vi.mock('@/components/shared/NetworkErrorBanner', () => ({ default: () => <div data-testid="network-banner" /> }))
vi.mock('@/components/overlays/ConfigInitBanner', () => ({ default: () => <div data-testid="config-banner" /> }))
vi.mock('./Sidebar', () => ({ default: () => <aside data-testid="sidebar" /> }))
vi.mock('./StatusBar', () => ({ default: () => <footer data-testid="status-bar" /> }))
vi.mock('./Header', () => ({ default: () => <header data-testid="header" /> }))
vi.mock('./ProjectFrameHost', () => ({ default: () => <div data-testid="project-frame-host" /> }))
vi.mock('./ProjectTabBar', () => ({ default: () => <div data-testid="project-tab-bar" /> }))
vi.mock('./ProjectTabBarControls', () => ({
  SHELL_MAIN_PANEL_ID: 'shell-main-panel',
  handleShellKeyboardShortcut: vi.fn().mockReturnValue(false),
  useProjectTabBarController: () => ({
    dialogs: null,
    focusTab: vi.fn(),
    focusTabByIndex: vi.fn(),
    cycleTabs: vi.fn(),
    closeProjectTab: vi.fn().mockResolvedValue(undefined),
    closeActiveProjectTab: vi.fn(),
    restartProjectTab: vi.fn().mockResolvedValue(undefined),
    revealProject: vi.fn(),
    openProjectInNewWindow: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/hooks/useProjectTabRouteSync', () => ({
  readWorkspaceProjectId: (pathname: string) => {
    const match = /^\/projects\/([^/]+)\/workspace\/?$/.exec(pathname)
    return match?.[1] ?? null
  },
  useProjectTabRouteSync: vi.fn(),
}))

function renderLayout(pathname: string) {
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <Routes>
        <Route element={<MainLayout />}>
          <Route path="/" element={<div>home</div>} />
          <Route path="/projects/:id/workspace" element={<div>workspace</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('MainLayout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '(max-width: 768px)',
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    useLayoutStore.setState({
      sidebarOpen: true,
      quickMenuOpen: false,
      modelSwitcherOpen: false,
      chatMode: 'advanced',
      setSidebarOpen: vi.fn(),
      setQuickMenuOpen: vi.fn(),
      setModelSwitcherOpen: vi.fn(),
      toggleSidebar: vi.fn(),
      hydrateChatMode: vi.fn(),
    })
    useModelAutoModeStore.setState({
      hydrateAutoSelected: vi.fn().mockResolvedValue(undefined),
    })
    useSessionStore.setState({
      sessions: [],
      activeSessionId: null,
      loading: false,
      sessionsHasMore: false,
      sessionsLoadingMore: false,
      sessionsTotal: 0,
      fetchSessions: vi.fn().mockResolvedValue(undefined),
    })
    useSettingsStore.setState((state) => ({
      ...state,
      fetchSettings: vi.fn().mockResolvedValue(undefined),
      hydrateLanguage: vi.fn().mockResolvedValue(undefined),
    }))
    useServerStore.setState({
      connected: true,
      version: '1.0.0',
      startupMode: 'desktop',
      projectName: '',
      projectId: '',
      setConnected: vi.fn(),
      setVersion: vi.fn(),
      setServerInfo: vi.fn(),
      startHealthCheck: vi.fn().mockReturnValue(() => {}),
    })
    useProjectStore.setState({
      projects: [],
      activeProjectId: null,
      workspace: { cwd: '/workspace/main-app', version: '1.0.0' },
      loading: false,
      initDialogProject: null,
      _es: null,
      connectEvents: vi.fn(),
      disconnectEvents: vi.fn(),
      fetchWorkspace: vi.fn().mockResolvedValue(undefined),
    })
    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      focusRequestId: 0,
      order: [],
      lastMainRoute: '/',
      restore: vi.fn().mockResolvedValue(undefined),
    })
  })

  it('hides the parent sidebar, status bar and config banner on workspace routes', () => {
    renderLayout('/projects/proj-1/workspace')

    expect(screen.getByTestId('header')).toBeInTheDocument()
    expect(screen.getByTestId('network-banner')).toBeInTheDocument()
    expect(screen.getByTestId('project-tab-bar')).toBeInTheDocument()
    expect(screen.getByTestId('project-frame-host')).toBeInTheDocument()
    expect(screen.queryByTestId('config-banner')).not.toBeInTheDocument()
    expect(screen.queryByTestId('sidebar')).not.toBeInTheDocument()
    expect(screen.queryByTestId('status-bar')).not.toBeInTheDocument()
  })

  it('renders the full shell chrome on non-workspace routes', () => {
    renderLayout('/')

    expect(screen.getByTestId('config-banner')).toBeInTheDocument()
    expect(screen.getByTestId('sidebar')).toBeInTheDocument()
    expect(screen.getByTestId('status-bar')).toBeInTheDocument()
  })
})
