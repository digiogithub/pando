import '@/i18n'
import '@testing-library/jest-dom/vitest'
import { act, fireEvent, render, waitFor } from '@testing-library/react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import type { ProjectTab } from '@pando/client/types'
import ProjectFrameHost from './ProjectFrameHost'

function makeTab(projectId: string, name: string): ProjectTab {
  return {
    projectId,
    name,
    path: `/workspace/${projectId}`,
    state: 'running',
    webUrl: `/api/v1/projects/${projectId}/web/`,
    openedAt: '2026-10-01T20:00:00Z',
    busy: false,
  }
}

function RouteControls() {
  const navigate = useNavigate()
  return (
    <div>
      <button type="button" onClick={() => navigate('/chat')}>Go chat</button>
      <button type="button" onClick={() => navigate('/projects/proj-1/workspace')}>Go project one</button>
      <button type="button" onClick={() => navigate('/projects/proj-2/workspace')}>Go project two</button>
    </div>
  )
}

function renderHost(initialEntry = '/projects/proj-1/workspace') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <RouteControls />
      <ProjectFrameHost
        controller={{
          dialogs: null,
          focusTab: vi.fn(),
          focusTabByIndex: vi.fn(),
          cycleTabs: vi.fn(),
          closeProjectTab: vi.fn().mockResolvedValue(undefined),
          closeActiveProjectTab: vi.fn(),
          restartProjectTab: vi.fn().mockResolvedValue(undefined),
          revealProject: vi.fn(),
          openProjectInNewWindow: vi.fn().mockResolvedValue(undefined),
        }}
        shortcutActions={{
          openQuickMenu: vi.fn(),
          openModelSwitcher: vi.fn(),
          toggleSidebar: vi.fn(),
          toggleAutoApprove: vi.fn(),
        }}
      />
    </MemoryRouter>,
  )
}

describe('ProjectFrameHost', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useSettingsStore.setState((state) => ({
      config: { ...state.config, language: 'en' },
      original: { ...state.original, language: 'en' },
    }))
    useProjectTabsStore.setState({
      tabs: [makeTab('proj-1', 'Project One'), makeTab('proj-2', 'Project Two')],
      activeTabId: 'proj-1',
      focusRequestId: 0,
      order: ['proj-1', 'proj-2'],
      lastMainRoute: '/',
    })
  })

  it('keeps frames mounted when switching the active workspace route', async () => {
    const { container, getByText } = renderHost()

    await waitFor(() => {
      expect(container.querySelectorAll('iframe.shell-projectframe')).toHaveLength(2)
    })

    const firstFrame = container.querySelector('iframe[title="Project One"]')
    const secondFrame = container.querySelector('iframe[title="Project Two"]')

    expect(firstFrame).not.toHaveAttribute('hidden')
    expect(secondFrame).toHaveAttribute('hidden')

    act(() => {
      useProjectTabsStore.getState().focusTab('proj-2')
    })
    fireEvent.click(getByText('Go project two'))

    await waitFor(() => {
      const first = container.querySelector('iframe[title="Project One"]')
      const second = container.querySelector('iframe[title="Project Two"]')
      expect(first).toBe(firstFrame)
      expect(second).toBe(secondFrame)
      expect(first).toHaveAttribute('hidden')
      expect(second).not.toHaveAttribute('hidden')
    })
  })

  it('removes only the closed frame', async () => {
    const { container } = renderHost()

    await waitFor(() => {
      expect(container.querySelectorAll('iframe.shell-projectframe')).toHaveLength(2)
    })

    const secondFrame = container.querySelector('iframe[title="Project Two"]')

    act(() => {
      useProjectTabsStore.setState((state) => ({
        tabs: state.tabs.filter((tab) => tab.projectId !== 'proj-1'),
        order: ['proj-2'],
      }))
    })

    await waitFor(() => {
      expect(container.querySelectorAll('iframe.shell-projectframe')).toHaveLength(1)
      expect(container.querySelector('iframe[title="Project One"]')).toBeNull()
      expect(container.querySelector('iframe[title="Project Two"]')).toBe(secondFrame)
    })
  })

  it('hides every frame when the route is not a workspace route', async () => {
    const { container, getByText } = renderHost()

    await waitFor(() => {
      expect(container.querySelectorAll('iframe.shell-projectframe')).toHaveLength(2)
    })

    fireEvent.click(getByText('Go chat'))

    await waitFor(() => {
      for (const frame of Array.from(container.querySelectorAll('iframe.shell-projectframe'))) {
        expect(frame).toHaveAttribute('hidden')
      }
    })
  })
})
