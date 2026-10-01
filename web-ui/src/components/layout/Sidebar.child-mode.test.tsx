import '@testing-library/jest-dom/vitest'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Sidebar from './Sidebar'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}))

describe('Sidebar child mode', () => {
  it('hides Projects and Instances navigation in project-child mode', () => {
    useServerStore.setState({ startupMode: 'project-child', projectName: 'Project One', projectId: 'project-1' })
    useSessionStore.setState({
      sessions: [],
      activeSessionId: null,
      loading: false,
      sessionsHasMore: false,
      sessionsLoadingMore: false,
      sessionsTotal: 0,
    })

    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    )

    expect(screen.queryByText('nav.projects')).not.toBeInTheDocument()
    expect(screen.queryByText('nav.instances')).not.toBeInTheDocument()
  })
})
