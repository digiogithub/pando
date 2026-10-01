import '@/i18n'
import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Project, ProjectTab } from '@pando/client/types'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import ProjectsView from './ProjectsView'

let desktopMode = false

vi.mock('@/services/desktop', () => ({
  get isDesktop() {
    return desktopMode
  },
}))

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: overrides.id ?? 'proj-1',
    name: overrides.name ?? 'Project One',
    path: overrides.path ?? '/workspace/project-one',
    status: overrides.status ?? 'stopped',
    initialized: overrides.initialized ?? true,
    web_state: overrides.web_state,
    web_port: overrides.web_port,
    external: overrides.external,
    delegations: overrides.delegations,
    delegation_spawned: overrides.delegation_spawned,
    created_at: overrides.created_at ?? 1,
    updated_at: overrides.updated_at ?? 1,
  }
}

function makeTab(overrides: Partial<ProjectTab> = {}): ProjectTab {
  return {
    projectId: overrides.projectId ?? 'proj-1',
    name: overrides.name ?? 'Project One',
    path: overrides.path ?? '/workspace/project-one',
    state: overrides.state ?? 'running',
    webUrl: overrides.webUrl ?? '/api/v1/projects/proj-1/web/',
    webPort: overrides.webPort ?? 4101,
    delegations: overrides.delegations ?? 0,
    openedAt: overrides.openedAt ?? '2026-10-01T20:00:00Z',
    error: overrides.error,
    busy: overrides.busy,
    childTitle: overrides.childTitle,
  }
}

describe('ProjectsView', () => {
  beforeEach(() => {
    desktopMode = false
    vi.restoreAllMocks()

    useProjectStore.setState({
      projects: [],
      activeProjectId: null,
      workspace: null,
      loading: false,
      initDialogProject: null,
      _es: null,
      fetchProjects: vi.fn().mockResolvedValue(undefined),
      fetchActive: vi.fn().mockResolvedValue(undefined),
      fetchWorkspace: vi.fn().mockResolvedValue(undefined),
      addProject: vi.fn().mockResolvedValue(undefined),
      activateProject: vi.fn().mockResolvedValue('ok'),
      stopProject: vi.fn().mockResolvedValue(true),
      openProjectDesktop: vi.fn().mockResolvedValue(undefined),
      deactivateProject: vi.fn().mockResolvedValue(undefined),
      initProject: vi.fn().mockResolvedValue(true),
      renameProject: vi.fn().mockResolvedValue(true),
      removeProject: vi.fn().mockResolvedValue(undefined),
      setInitDialogProject: (project) => useProjectStore.setState({ initDialogProject: project }),
      connectEvents: vi.fn(),
      disconnectEvents: vi.fn(),
    })

    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      focusRequestId: 0,
      order: [],
      lastMainRoute: '/',
      openTab: vi.fn().mockResolvedValue({ ok: true, code: 'opened' }),
      closeTab: vi.fn().mockResolvedValue({ ok: true, code: 'removed' }),
      focusTab: vi.fn(),
      setRuntimeState: vi.fn(),
      reorder: vi.fn(),
      restore: vi.fn(),
      applyEvent: vi.fn(),
      restartTab: vi.fn(),
    })
  })

  it('opens a project tab on row click', async () => {
    const project = makeProject()
    const openTab = vi.fn().mockResolvedValue({ ok: true, code: 'opened' })

    useProjectStore.setState({ projects: [project] })
    useProjectTabsStore.setState({ openTab })

    render(<ProjectsView />)

    fireEvent.click(screen.getByText('Project One').closest('tr') as HTMLElement)

    await waitFor(() => expect(openTab).toHaveBeenCalledWith('proj-1'))
  })

  it('focuses an already open project tab from a row click through the tabs store', async () => {
    const project = makeProject({ web_state: 'running', web_port: 4101 })
    const focusTab = vi.fn()
    const openTab = vi.fn().mockImplementation(async (projectId: string) => {
      useProjectTabsStore.getState().focusTab(projectId)
      return { ok: true, code: 'already_open', tab: makeTab() }
    })

    useProjectStore.setState({ projects: [project] })
    useProjectTabsStore.setState({
      tabs: [makeTab()],
      openTab,
      focusTab,
    })

    render(<ProjectsView />)

    fireEvent.click(screen.getByText('Project One').closest('tr') as HTMLElement)

    await waitFor(() => {
      expect(openTab).toHaveBeenCalledWith('proj-1')
      expect(focusTab).toHaveBeenCalledWith('proj-1')
    })
  })

  it('stops a project and closes its tab after confirmation', async () => {
    const project = makeProject({ status: 'running', web_state: 'running', delegations: 2 })
    const stopProject = vi.fn().mockResolvedValue(true)
    const closeTab = vi.fn().mockResolvedValue({ ok: true, code: 'removed' })

    useProjectStore.setState({
      projects: [project],
      stopProject,
    })
    useProjectTabsStore.setState({
      tabs: [makeTab({ delegations: 2 })],
      closeTab,
    })

    render(<ProjectsView />)

    fireEvent.click(screen.getByLabelText('Stop'))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Stop' }))

    await waitFor(() => {
      expect(stopProject).toHaveBeenCalledWith('proj-1')
      expect(closeTab).toHaveBeenCalledWith('proj-1')
    })
  })

  it('uses the init wizard flow before opening an uninitialized project', async () => {
    const project = makeProject({ initialized: false })
    const initProject = vi.fn().mockResolvedValue(true)
    const openTab = vi.fn().mockImplementation(async () => {
      useProjectStore.getState().setInitDialogProject(project)
      return {
        ok: false,
        code: 'error',
        notice: { key: 'projects.tabs.needsInit', type: 'warning' },
      }
    })

    useProjectStore.setState({
      projects: [project],
      initProject,
    })
    useProjectTabsStore.setState({ openTab })

    render(<ProjectsView />)

    fireEvent.click(screen.getByText('Project One').closest('tr') as HTMLElement)
    fireEvent.click(await screen.findByRole('button', { name: 'Initialize & open' }))

    await waitFor(() => expect(initProject).toHaveBeenCalledWith('proj-1'))
    expect(initProject).not.toHaveBeenCalledWith('proj-1', { activateAfter: true })
  })

  it('shows the desktop window action only in desktop mode', async () => {
    const project = makeProject()
    const openProjectDesktop = vi.fn().mockResolvedValue(undefined)
    useProjectStore.setState({ projects: [project], openProjectDesktop })

    const { rerender } = render(<ProjectsView />)
    expect(screen.queryByLabelText('Open in new window')).not.toBeInTheDocument()

    desktopMode = true
    rerender(<ProjectsView />)

    fireEvent.click(await screen.findByLabelText('Open in new window'))

    await waitFor(() => expect(openProjectDesktop).toHaveBeenCalledWith('proj-1'))
  })
})
