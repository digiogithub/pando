import { expect, test } from '@playwright/test'

const baseURL = process.env.PANDO_E2E_BASE_URL

test.describe('project tabs', () => {
  test.skip(!baseURL, 'PANDO_E2E_BASE_URL not set')

  test.beforeEach(async ({ page }) => {
    const token = process.env.PANDO_E2E_TOKEN
    if (token) {
      await page.addInitScript((value) => localStorage.setItem('pando_token', value), token)
    }

    await page.addInitScript(() => {
      localStorage.setItem(
        'pando_project_tabs',
        JSON.stringify({
          order: ['proj-1', 'proj-2'],
          activeTabId: 'main',
          lastMainRoute: '/',
        }),
      )
    })
  })

  test('switches between tabs, closes with stop, and hides after the last tab closes', async ({ page }) => {
    await page.route('**/api/v1/projects/web', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          instances: [
            {
              project_id: 'proj-1',
              name: 'Project One',
              path: '/workspace/project-one',
              state: 'running',
              started_at: '2026-10-01T20:00:00Z',
              delegations: 0,
            },
            {
              project_id: 'proj-2',
              name: 'Project Two',
              path: '/workspace/project-two',
              state: 'running',
              started_at: '2026-10-01T20:01:00Z',
              delegations: 1,
            },
          ],
        }),
      })
    })

    await page.route('**/api/v1/project', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          cwd: '/workspace/main-app',
          version: '1.0.0',
        }),
      })
    })

    await page.route('**/api/v1/projects/*/web/close', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'closed',
          cancelled_delegations: 0,
        }),
      })
    })

    await page.goto('/')
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toBeVisible()
    await expect(page.getByRole('tab', { name: /main-app/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /Project One/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /Project Two/i })).toBeVisible()

    await page.getByRole('tab', { name: /Project One/i }).click()
    await expect(page.getByRole('tab', { name: /Project One/i })).toHaveAttribute('aria-selected', 'true')

    await page.getByRole('button', { name: 'Close current project tab' }).first().click()
    await page.getByRole('button', { name: 'Close and stop workspace' }).click()
    await expect(page.getByRole('tab', { name: /Project One/i })).toHaveCount(0)

    await page.getByRole('button', { name: 'Close current project tab' }).click()
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toHaveCount(0)
  })
})
