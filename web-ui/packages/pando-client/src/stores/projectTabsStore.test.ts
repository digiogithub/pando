import { beforeEach, describe, expect, it, vi } from 'vitest'
import api, { setBaseURL } from '../services/api'
import type { Project, ProjectTab, ProjectWebInstance } from '../types'
import { useProjectStore } from './projectStore'
import { useProjectTabsStore } from './projectTabsStore'

function makeProject(id: string, name = id): Project {
  return {
    id,
    name,
    path: `/workspace/${id}`,
    status: 'stopped',
    initialized: true,
    created_at: 1,
    updated_at: 1,
  }
}

function makeTab(projectId: string, overrides: Partial<ProjectTab> = {}): ProjectTab {
  return {
    projectId,
    name: overrides.name ?? projectId,
    path: overrides.path ?? `/workspace/${projectId}`,
    state: overrides.state ?? 'running',
    webUrl: overrides.webUrl ?? `/api/v1/projects/${projectId}/web/`,
    webPort: overrides.webPort ?? 4100,
    delegations: overrides.delegations ?? 0,
    openedAt: overrides.openedAt ?? '2026-10-01T20:00:00Z',
    error: overrides.error,
  }
}

function flushPromises() {
  return new Promise((resolve) => setTimeout(resolve, 0))
}

describe('projectTabsStore', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    setBaseURL('')
    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      order: [],
      lastMainRoute: '/',
    })
    useProjectStore.setState({
      projects: [],
      activeProjectId: null,
      workspace: null,
      loading: false,
      initDialogProject: null,
      _es: null,
    })
    window.localStorage.clear()
  })

  it('opens a tab when the workspace starts successfully', async () => {
    useProjectStore.setState({ projects: [makeProject('proj-1', 'Project One')] })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      status: 'opened',
      project_id: 'proj-1',
      web_url: '/api/v1/projects/proj-1/web/',
      web_port: 4310,
    })

    const result = await useProjectTabsStore.getState().openTab('proj-1')

    expect(post).toHaveBeenCalledWith('/api/v1/projects/proj-1/web/open', {})
    expect(result.ok).toBe(true)
    expect(result.code).toBe('opened')
    expect(result.notice?.key).toBe('projects.tabs.started')
    expect(useProjectTabsStore.getState().activeTabId).toBe('proj-1')
    expect(useProjectTabsStore.getState().tabs).toEqual([
      expect.objectContaining({
        projectId: 'proj-1',
        name: 'Project One',
        state: 'running',
        webUrl: '/api/v1/projects/proj-1/web/',
        webPort: 4310,
      }),
    ])
  })

  it('deduplicates an already open tab response', async () => {
    useProjectStore.setState({ projects: [makeProject('proj-1')] })
    vi.spyOn(api, 'post').mockResolvedValue({
      status: 'already_open',
      project_id: 'proj-1',
      web_url: '/api/v1/projects/proj-1/web/',
      web_port: 4310,
    })

    const result = await useProjectTabsStore.getState().openTab('proj-1')

    expect(result.ok).toBe(true)
    expect(result.code).toBe('already_open')
    expect(useProjectTabsStore.getState().tabs).toHaveLength(1)
    expect(useProjectTabsStore.getState().tabs[0]?.state).toBe('running')
  })

  it('retries after project initialization succeeds', async () => {
    useProjectStore.setState({ projects: [makeProject('proj-1', 'Project One')] })

    const post = vi.spyOn(api, 'post').mockImplementation(async (path: string) => {
      if (path === '/api/v1/projects/proj-1/web/open') {
        const count = post.mock.calls.filter(([calledPath]) => calledPath === path).length
        if (count === 1) {
          throw new Error(JSON.stringify({ error: 'project_needs_init', project_id: 'proj-1' }))
        }
        return {
          status: 'opened',
          project_id: 'proj-1',
          web_url: '/api/v1/projects/proj-1/web/',
          web_port: 4310,
        }
      }

      if (path === '/api/v1/projects/proj-1/init') {
        return {}
      }

      throw new Error(`Unexpected POST ${path}`)
    })

    vi.spyOn(api, 'get').mockImplementation(async (path: string) => {
      if (path === '/api/v1/projects') {
        return { projects: [makeProject('proj-1', 'Project One')] }
      }
      throw new Error(`Unexpected GET ${path}`)
    })

    const openPromise = useProjectTabsStore.getState().openTab('proj-1')
    await flushPromises()

    expect(useProjectStore.getState().initDialogProject?.id).toBe('proj-1')

    const initialized = await useProjectStore.getState().initProject('proj-1')
    const result = await openPromise

    expect(initialized).toBe(true)
    expect(result.ok).toBe(true)
    expect(result.code).toBe('opened')
    expect(post.mock.calls.filter(([path]) => path === '/api/v1/projects/proj-1/web/open')).toHaveLength(2)
    expect(useProjectTabsStore.getState().tabs[0]?.state).toBe('running')
  })

  it('marks a tab as error when opening fails', async () => {
    useProjectStore.setState({ projects: [makeProject('proj-1')] })
    vi.spyOn(api, 'post').mockRejectedValue(
      new Error(JSON.stringify({ error: 'child_startup_failed', detail: 'stderr tail' })),
    )

    const result = await useProjectTabsStore.getState().openTab('proj-1')

    expect(result.ok).toBe(false)
    expect(result.code).toBe('error')
    expect(result.error).toBe('stderr tail')
    expect(result.notice?.key).toBe('projects.tabs.error')
    expect(useProjectTabsStore.getState().tabs[0]).toEqual(
      expect.objectContaining({
        projectId: 'proj-1',
        state: 'error',
        error: 'stderr tail',
      }),
    )
  })

  it('closes a tab locally without stopping the child instance', async () => {
    const post = vi.spyOn(api, 'post')
    useProjectTabsStore.setState({
      tabs: [makeTab('proj-1')],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      lastMainRoute: '/chat',
    })

    const result = await useProjectTabsStore.getState().closeTab('proj-1')

    expect(result.ok).toBe(true)
    expect(result.code).toBe('removed')
    expect(post).not.toHaveBeenCalled()
    expect(useProjectTabsStore.getState().tabs).toEqual([])
    expect(useProjectTabsStore.getState().activeTabId).toBe('main')
  })

  it('stops the child instance when closing with stop=true', async () => {
    useProjectTabsStore.setState({
      tabs: [makeTab('proj-1')],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      lastMainRoute: '/chat',
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      status: 'closed',
      project_id: 'proj-1',
      cancelled_delegations: 0,
    })

    const result = await useProjectTabsStore.getState().closeTab('proj-1', { stop: true })

    expect(post).toHaveBeenCalledWith('/api/v1/projects/proj-1/web/close', {})
    expect(result.ok).toBe(true)
    expect(result.code).toBe('closed')
    expect(result.notice?.key).toBe('projects.tabs.stopped')
    expect(useProjectTabsStore.getState().tabs).toEqual([])
  })

  it('deduplicates concurrent open requests for the same project', async () => {
    useProjectStore.setState({ projects: [makeProject('proj-1')] })

    let resolveOpen!: (value: {
      status: 'opened'
      project_id: string
      web_url: string
      web_port: number
    }) => void
    const post = vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveOpen = resolve
        }) as Promise<unknown>,
    )

    const first = useProjectTabsStore.getState().openTab('proj-1')
    const second = useProjectTabsStore.getState().openTab('proj-1')
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    resolveOpen({
      status: 'opened',
      project_id: 'proj-1',
      web_url: '/api/v1/projects/proj-1/web/',
      web_port: 4310,
    })

    const [firstResult, secondResult] = await Promise.all([first, second])
    expect(firstResult.code).toBe('opened')
    expect(secondResult.code).toBe('opened')
    expect(useProjectTabsStore.getState().tabs).toHaveLength(1)
  })

  it('restores tabs from the server and merges persisted order', async () => {
    window.localStorage.setItem(
      'pando_project_tabs',
      JSON.stringify({
        order: ['proj-2', 'proj-1', 'missing'],
        activeTabId: 'proj-2',
        lastMainRoute: '/settings',
      }),
    )

    const instances: ProjectWebInstance[] = [
      {
        project_id: 'proj-1',
        name: 'One',
        path: '/workspace/proj-1',
        state: 'running',
        started_at: '2026-10-01T20:00:00Z',
      },
      {
        project_id: 'proj-2',
        name: 'Two',
        path: '/workspace/proj-2',
        state: 'starting',
        started_at: '2026-10-01T20:01:00Z',
      },
      {
        project_id: 'proj-3',
        name: 'Three',
        path: '/workspace/proj-3',
        state: 'error',
        started_at: '2026-10-01T20:02:00Z',
        error: 'boom',
      },
    ]

    vi.spyOn(api, 'get').mockResolvedValue({ instances })

    await useProjectTabsStore.getState().restore()

    expect(useProjectTabsStore.getState().order).toEqual(['proj-2', 'proj-1', 'proj-3'])
    expect(useProjectTabsStore.getState().tabs.map((tab) => tab.projectId)).toEqual([
      'proj-2',
      'proj-1',
      'proj-3',
    ])
    expect(useProjectTabsStore.getState().activeTabId).toBe('proj-2')
    expect(useProjectTabsStore.getState().lastMainRoute).toBe('/settings')
  })

  it('applies lifecycle and delegation events to tabs', () => {
    useProjectStore.setState({
      projects: [makeProject('proj-1', 'Project One'), makeProject('proj-2', 'Project Two')],
    })
    useProjectTabsStore.setState({
      tabs: [makeTab('proj-1')],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      lastMainRoute: '/chat',
    })

    useProjectTabsStore.getState().applyEvent({
      type: 'delegation_changed',
      projectId: 'proj-1',
      delegations: 3,
    })
    expect(useProjectTabsStore.getState().tabs[0]?.delegations).toBe(3)

    useProjectTabsStore.getState().applyEvent({
      type: 'status_changed',
      projectId: 'proj-1',
      status: 'starting',
    })
    expect(useProjectTabsStore.getState().tabs[0]?.state).toBe('starting')

    useProjectTabsStore.getState().applyEvent({
      type: 'web_error',
      projectId: 'proj-1',
      error: 'startup failed',
      webPort: 4310,
    })
    expect(useProjectTabsStore.getState().tabs[0]).toEqual(
      expect.objectContaining({
        state: 'error',
        error: 'startup failed',
        webPort: 4310,
      }),
    )

    useProjectTabsStore.getState().applyEvent({
      type: 'web_stopped',
      projectId: 'proj-1',
      webPort: 4310,
    })
    expect(useProjectTabsStore.getState().tabs[0]).toEqual(
      expect.objectContaining({
        state: 'stopped',
        delegations: 0,
      }),
    )

    useProjectTabsStore.getState().applyEvent({
      type: 'web_started',
      projectId: 'proj-2',
      webPort: 5320,
    })
    expect(useProjectTabsStore.getState().tabs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          projectId: 'proj-2',
          name: 'Project Two',
          state: 'running',
          webPort: 5320,
        }),
      ]),
    )
  })

  it('stays inert in project child mode', async () => {
    setBaseURL('/api/v1/projects/proj-1/web')
    const get = vi.spyOn(api, 'get')
    const post = vi.spyOn(api, 'post')

    useProjectTabsStore.setState({
      tabs: [makeTab('proj-1')],
      activeTabId: 'proj-1',
      order: ['proj-1'],
      lastMainRoute: '/chat',
    })

    await useProjectTabsStore.getState().restore()
    const result = await useProjectTabsStore.getState().openTab('proj-1')

    expect(get).not.toHaveBeenCalled()
    expect(post).not.toHaveBeenCalled()
    expect(result.code).toBe('noop')
    expect(useProjectTabsStore.getState().tabs).toEqual([])
    expect(useProjectTabsStore.getState().activeTabId).toBe('main')
  })

  it('persists active tab, order and last main route across updates', async () => {
    const tabOne = makeTab('proj-1')
    const tabTwo = makeTab('proj-2', { webPort: 4320 })
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      instances: [
        {
          project_id: 'proj-1',
          name: 'proj-1',
          path: '/workspace/proj-1',
          state: 'running',
          started_at: tabOne.openedAt,
        },
        {
          project_id: 'proj-2',
          name: 'proj-2',
          path: '/workspace/proj-2',
          state: 'running',
          started_at: tabTwo.openedAt,
        },
      ] satisfies ProjectWebInstance[],
    })

    useProjectTabsStore.setState({
      tabs: [tabOne, tabTwo],
      activeTabId: 'main',
      order: ['proj-1', 'proj-2'],
      lastMainRoute: '/chat',
    })

    useProjectTabsStore.getState().focusTab('proj-2')
    useProjectTabsStore.getState().reorder(['proj-2', 'proj-1'])
    useProjectTabsStore.setState({ lastMainRoute: '/settings' })

    const persisted = JSON.parse(window.localStorage.getItem('pando_project_tabs') ?? '{}')

    expect(persisted).toEqual({
      order: ['proj-2', 'proj-1'],
      activeTabId: 'proj-2',
      lastMainRoute: '/settings',
    })

    useProjectTabsStore.setState({
      tabs: [],
      activeTabId: 'main',
      order: [],
      lastMainRoute: '/',
    })
    window.localStorage.setItem('pando_project_tabs', JSON.stringify(persisted))

    await useProjectTabsStore.getState().restore()

    expect(get).toHaveBeenCalledWith('/api/v1/projects/web')
    expect(useProjectTabsStore.getState().order).toEqual(['proj-2', 'proj-1'])
    expect(useProjectTabsStore.getState().activeTabId).toBe('proj-2')
    expect(useProjectTabsStore.getState().lastMainRoute).toBe('/settings')
  })
})
