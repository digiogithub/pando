import '@/i18n'
import '@testing-library/jest-dom/vitest'
import { useEffect } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setBaseURL } from '@pando/client/services/api'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import ProjectTabBar from './ProjectTabBar'
import {
  handleProjectTabKeyboardShortcut,
  useProjectTabBarController,
} from './ProjectTabBarControls'

vi.mock('@/services/desktop', () => ({
  isDesktop: false,
}))

function renderProjectTabBar() {
  function Harness() {
    const controller = useProjectTabBarController()

    useEffect(() => {
      const onKeyDown = (event: KeyboardEvent) => {
        if (handleProjectTabKeyboardShortcut(event, controller)) {
          event.preventDefault()
        }
      }
      window.addEventListener('keydown', onKeyDown)
      return () => window.removeEventListener('keydown', onKeyDown)
    }, [controller])

    return (
      <>
        <ProjectTabBar controller={controller} />
        {controller.dialogs}
      </>
    )
  }

  return render(
    <MemoryRouter>
      <Harness />
    </MemoryRouter>,
  )
}

describe('ProjectTabBar', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    setBaseURL('')
    window.localStorage.clear()
    window.sessionStorage.clear()
    useProjectStore.setState({
      projects: [],
      activeProjectId: null,
      workspace: { cwd: '/workspace/main-app', version: '1.0.0' },
      loading: false,
      initDialogProject: null,
      _es: null,
    })
    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      order: [],
      lastMainRoute: '/',
      openTab: vi.fn(),
      closeTab: vi.fn(),
      focusTab: vi.fn(),
      reorder: vi.fn(),
      restore: vi.fn(),
      applyEvent: vi.fn(),
      restartTab: vi.fn(),
    })
  })

  it('renders the home tab and project tabs from the store', async () => {
    useProjectTabsStore.setState({
      tabs: [
        {
          projectId: 'proj-1',
          name: 'Project One',
          path: '/workspace/project-one',
          state: 'running',
          webUrl: '/api/v1/projects/proj-1/web/',
          webPort: 4101,
          delegations: 2,
          openedAt: '2026-10-01T20:00:00Z',
        },
        {
          projectId: 'proj-2',
          name: 'Project Two',
          path: '/workspace/project-two',
          state: 'starting',
          webUrl: '/api/v1/projects/proj-2/web/',
          webPort: 4102,
          delegations: 0,
          openedAt: '2026-10-01T20:01:00Z',
        },
      ],
      activeTabId: 'proj-1',
      order: ['proj-1', 'proj-2'],
    })

    renderProjectTabBar()

    expect(await screen.findByRole('tablist', { name: 'Project tabs' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /main-app/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Project One/i })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: /Project Two/i })).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('hides when there are no open project tabs', () => {
    renderProjectTabBar()
    expect(screen.queryByRole('tablist', { name: 'Project tabs' })).not.toBeInTheDocument()
  })

  it('confirms close with stop and calls closeTab with the stop choice', async () => {
    const closeTab = vi.fn().mockResolvedValue({
      ok: true,
      code: 'closed',
      notice: { key: 'projects.tabs.stopped', type: 'info' },
    })
    useProjectTabsStore.setState({
      tabs: [
        {
          projectId: 'proj-1',
          name: 'Project One',
          path: '/workspace/project-one',
          state: 'running',
          webUrl: '/api/v1/projects/proj-1/web/',
          webPort: 4101,
          delegations: 0,
          openedAt: '2026-10-01T20:00:00Z',
        },
      ],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      closeTab,
    })

    renderProjectTabBar()

    fireEvent.click(screen.getByRole('button', { name: 'Close current project tab' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Close and stop workspace' }))

    await waitFor(() =>
      expect(closeTab).toHaveBeenCalledWith('proj-1', { stop: true }),
    )
  })

  it('middle-click closes a tab using the remembered keep-running choice', async () => {
    window.sessionStorage.setItem('pando_project_tab_close_choice', JSON.stringify({ stop: 'keep' }))
    const closeTab = vi.fn().mockResolvedValue({
      ok: true,
      code: 'removed',
    })
    useProjectTabsStore.setState({
      tabs: [
        {
          projectId: 'proj-1',
          name: 'Project One',
          path: '/workspace/project-one',
          state: 'running',
          webUrl: '/api/v1/projects/proj-1/web/',
          webPort: 4101,
          delegations: 0,
          openedAt: '2026-10-01T20:00:00Z',
        },
      ],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      closeTab,
    })

    renderProjectTabBar()

    fireEvent.mouseDown(screen.getByRole('tab', { name: /Project One/i }), { button: 1 })

    await waitFor(() => expect(closeTab).toHaveBeenCalledWith('proj-1', undefined))
  })

  it('handles keyboard shortcuts for focusing, closing and cycling tabs', async () => {
    window.sessionStorage.setItem('pando_project_tab_close_choice', JSON.stringify({ stop: 'keep' }))
    const closeTab = vi.fn().mockResolvedValue({ ok: true, code: 'removed' })
    const focusTab = vi.fn((id: 'main' | string) => {
      useProjectTabsStore.setState({ activeTabId: id })
    })
    useProjectTabsStore.setState({
      tabs: [
        {
          projectId: 'proj-1',
          name: 'Project One',
          path: '/workspace/project-one',
          state: 'running',
          webUrl: '/api/v1/projects/proj-1/web/',
          webPort: 4101,
          delegations: 0,
          openedAt: '2026-10-01T20:00:00Z',
        },
        {
          projectId: 'proj-2',
          name: 'Project Two',
          path: '/workspace/project-two',
          state: 'running',
          webUrl: '/api/v1/projects/proj-2/web/',
          webPort: 4102,
          delegations: 0,
          openedAt: '2026-10-01T20:01:00Z',
        },
      ],
      activeTabId: 'main',
      order: ['proj-1', 'proj-2'],
      closeTab,
      focusTab,
    })

    renderProjectTabBar()

    fireEvent.keyDown(window, { ctrlKey: true, altKey: true, code: 'Digit2', key: '2' })
    await waitFor(() => expect(focusTab).toHaveBeenCalledWith('proj-1'))

    fireEvent.keyDown(window, { ctrlKey: true, altKey: true, key: 'ArrowRight' })
    await waitFor(() => expect(focusTab).toHaveBeenLastCalledWith('proj-2'))

    fireEvent.keyDown(window, { ctrlKey: true, altKey: true, code: 'KeyW', key: 'w' })
    await waitFor(() => expect(closeTab).toHaveBeenCalledWith('proj-2', undefined))
  })
})
