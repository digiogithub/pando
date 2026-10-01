import { beforeEach, describe, expect, it } from 'vitest'
import { isChildModeRestrictedPath, isProjectChildMode, setBaseURL, setServerProjectChildMode } from './api'

describe('project child mode detection', () => {
  beforeEach(() => {
    setBaseURL('')
    setServerProjectChildMode('')
  })

  it('turns on child mode when the server reports project-child', () => {
    expect(isProjectChildMode()).toBe(false)
    setServerProjectChildMode('project-child')
    expect(isProjectChildMode()).toBe(true)
  })

  it('keeps base-path child detection working', () => {
    setBaseURL('/api/v1/projects/project-1/web')
    expect(isProjectChildMode()).toBe(true)
  })

  it('marks project-management routes as restricted in child mode', () => {
    expect(isChildModeRestrictedPath('/projects')).toBe(true)
    expect(isChildModeRestrictedPath('/instances')).toBe(true)
    expect(isChildModeRestrictedPath('/settings')).toBe(false)
  })
})
