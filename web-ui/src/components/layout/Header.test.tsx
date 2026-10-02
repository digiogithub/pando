import '@/i18n'
import '@testing-library/jest-dom/vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import Header from './Header'

const syncDesktopShellState = vi.fn()

vi.mock('@/components/shared/PersonaSelector', () => ({ default: () => <div data-testid="persona-selector" /> }))
vi.mock('./DesktopWindowControls', () => ({ default: () => <div data-testid="window-controls" /> }))
vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ resolvedMode: 'dark', toggleMode: vi.fn() }),
}))
vi.mock('@/hooks/useAgentBusy', () => ({
  useAgentBusy: () => false,
}))
vi.mock('./headerSections', () => ({
  resolveHeaderSection: () => 'Chat',
}))
vi.mock('@/services/desktopWindow', () => ({
  onTitleBarDoubleClick: vi.fn(),
  syncDesktopShellState: (...args: unknown[]) => syncDesktopShellState(...args),
  useDesktopShell: () => true,
}))

function renderHeader(props?: Partial<Parameters<typeof Header>[0]>) {
  return render(
    <MemoryRouter>
      <Header {...props} />
    </MemoryRouter>,
  )
}

describe('Header', () => {
  const originalParent = window.parent

  beforeEach(() => {
    vi.clearAllMocks()
    useLayoutStore.setState({
      sidebarOpen: true,
      chatMode: 'advanced',
      toggleSidebar: vi.fn(),
      setChatMode: vi.fn(),
    })
    useSessionStore.setState({
      sessions: [],
      activeSessionId: null,
    })
    useServerStore.setState({
      version: '1.0.0',
      startupMode: 'desktop',
      projectName: '',
      projectId: '',
    })
    useExtensionPanelsStore.setState({ panels: [] })
    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      order: [],
      focusRequestId: 0,
      lastMainRoute: '/',
    })
    Object.defineProperty(window, 'parent', { value: originalParent, configurable: true })
  })

  it('renders the project-child embedded variant without brand or desktop controls', () => {
    useServerStore.setState({
      startupMode: 'project-child',
      projectName: 'Project One',
      projectId: 'proj-1',
    })
    Object.defineProperty(window, 'parent', { value: {} as Window, configurable: true })

    renderHeader()

    expect(screen.queryByLabelText('Pando')).not.toBeInTheDocument()
    expect(screen.queryByTestId('window-controls')).not.toBeInTheDocument()
    expect(screen.getByTestId('persona-selector')).toBeInTheDocument()
  })

  it('syncs the desktop shell state and can hide the sidebar toggle', () => {
    useProjectTabsStore.setState({
      tabs: [{ projectId: 'proj-1', name: 'Project One', path: '', state: 'running', openedAt: '2026-10-02T08:00:00Z' }],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      focusRequestId: 0,
      lastMainRoute: '/',
    })

    renderHeader({ hideSidebarToggle: true })

    expect(screen.queryByLabelText(/expand sidebar|collapse sidebar|open menu/i)).not.toBeInTheDocument()
    expect(syncDesktopShellState).toHaveBeenCalledWith({
      title: 'Pando — Project One / Chat',
      activeTabId: 'proj-1',
      projectTabs: [{ projectId: 'proj-1', name: 'Project One' }],
    })
  })
})
