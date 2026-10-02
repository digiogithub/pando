import path from 'node:path'
import { expect, test, type Page } from '@playwright/test'

const baseURL = process.env.PANDO_E2E_BASE_URL
const projectDir = process.env.PANDO_E2E_PROJECT_DIR

async function dismissSetupAssistantIfPresent(page: Page) {
  const cancel = page.getByRole('button', { name: 'Cancel assistant', exact: true })
  const visible = await cancel.waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false)
  if (visible) {
    await cancel.click()
    await expect(page.locator('.ui-dialog-overlay')).toHaveCount(0)
  }
}

test.describe('project tabs', () => {
  test.skip(!baseURL, 'PANDO_E2E_BASE_URL not set')
  test.skip(!projectDir, 'PANDO_E2E_PROJECT_DIR not set')

  test('opens, keeps alive, restores, and stops a project workspace tab', async ({ page, request }) => {
    const projectName = path.basename(projectDir!)

    const tokenResponse = await request.get('/api/v1/token')
    expect(tokenResponse.ok()).toBeTruthy()
    const tokenPayload = await tokenResponse.json() as { token: string }
    expect(tokenPayload.token).toBeTruthy()
    const token = tokenPayload.token

    const apiHeaders = {
      'Content-Type': 'application/json',
      'X-Pando-Token': token,
    }

    const createResponse = await request.post('/api/v1/projects', {
      headers: apiHeaders,
      data: { name: projectName, path: projectDir },
    })
    expect(createResponse.ok()).toBeTruthy()
    const createPayload = await createResponse.json() as { project: { id: string } }
    const projectID = createPayload.project.id

    const initResponse = await request.post(`/api/v1/projects/${projectID}/init`, {
      headers: apiHeaders,
    })
    expect(initResponse.ok()).toBeTruthy()

    await page.addInitScript((value) => localStorage.setItem('pando_token', value), token)
    await page.goto('/projects')
    await dismissSetupAssistantIfPresent(page)

    const row = page.locator('tbody tr').filter({ hasText: projectDir! })
    await expect(row).toHaveCount(1)
    await row.click()

    await expect(page).toHaveURL(new RegExp(`/projects/${projectID}/workspace$`))
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toBeVisible()

    const frameSelector = `iframe[title="${projectName}"]`
    const frameElement = page.locator(frameSelector)
    const frame = page.frameLocator(frameSelector)
    await expect(frameElement).toBeVisible()

    await expect(frame.getByRole('link', { name: 'Projects', exact: true })).toHaveCount(0)
    await expect(frame.getByRole('link', { name: 'Instances', exact: true })).toHaveCount(0)
    await expect(frame.getByRole('tablist', { name: 'Project tabs' })).toHaveCount(0)

    await frame.getByRole('link', { name: 'Terminal', exact: true }).click()
    await expect(frame.locator('.xterm').first()).toBeVisible()

    await expect.poll(async () => page.evaluate(() => window.localStorage.getItem('pando_token'))).toBe(token)
    await expect.poll(async () =>
      page.evaluate(() => Object.keys(window.localStorage).some((key) => key.startsWith('pando_token@'))),
    ).toBe(true)

    const childTerminalPath = await frameElement.evaluate((element) =>
      (element as HTMLIFrameElement).contentWindow?.location.pathname ?? '',
    )
    expect(childTerminalPath).toContain('/terminal')

    await page.getByRole('tab').first().click()
    await expect(page).not.toHaveURL(new RegExp(`/projects/${projectID}/workspace$`))

    await page.getByRole('tab', { name: new RegExp(projectName, 'i') }).click()
    await expect(page).toHaveURL(new RegExp(`/projects/${projectID}/workspace$`))
    await expect(frame.locator('.xterm').first()).toBeVisible()
    await expect
      .poll(async () => frameElement.evaluate((element) =>
        (element as HTMLIFrameElement).contentWindow?.location.pathname ?? '',
      ))
      .toBe(childTerminalPath)

    page.on('dialog', (dialog) => void dialog.accept())
    await page.reload()
    await dismissSetupAssistantIfPresent(page)
    await expect(page).toHaveURL(new RegExp(`/projects/${projectID}/workspace$`))
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toBeVisible()
    await expect(page.getByRole('tab', { name: new RegExp(projectName, 'i') })).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator(frameSelector)).toBeVisible()

    await page.getByRole('button', { name: 'Close current project tab' }).click({ force: true })
    await page.getByRole('button', { name: 'Close and stop workspace' }).click()
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toHaveCount(0)

    const instancesResponse = await request.get('/api/v1/projects/web', {
      headers: { 'X-Pando-Token': token },
    })
    expect(instancesResponse.ok()).toBeTruthy()
    const instancesPayload = await instancesResponse.json() as { instances: Array<unknown> }
    expect(instancesPayload.instances).toEqual([])
  })
})
