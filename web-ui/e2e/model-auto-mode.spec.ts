import { expect, test } from '@playwright/test'

/**
 * Auto mode settings flow. Needs a Pando started with `pando app` (not `serve`)
 * in an isolated HOME, whose model-auto-mode decision provider points at a fake
 * Jev server (custom provider). Skipped unless PANDO_E2E_BASE_URL is set.
 *
 * Optional: PANDO_E2E_TOKEN (API token stored as pando_token),
 *           PANDO_E2E_FAKE_JEV_URL (base URL of the fake decision server).
 */
const baseURL = process.env.PANDO_E2E_BASE_URL
const fakeJev = process.env.PANDO_E2E_FAKE_JEV_URL

test.describe('model auto mode', () => {
  test.skip(!baseURL, 'PANDO_E2E_BASE_URL not set')

  test.beforeEach(async ({ page }) => {
    const token = process.env.PANDO_E2E_TOKEN
    if (token) await page.addInitScript((t) => localStorage.setItem('pando_token', t), token)
  })

  test('configure routes, test connection, route in playground, save, see Auto first', async ({ page }) => {
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Auto mode' }).click()

    await page.getByRole('switch', { name: /Enable Auto mode/ }).click()
    await page.getByLabel('Provider').selectOption('custom')
    if (fakeJev) await page.getByLabel('Base URL').fill(fakeJev)
    await page.getByLabel('Decision model').fill('fake-jev')

    await page.getByRole('button', { name: 'Test connection' }).click()
    await expect(page.getByTestId('ama-test-report')).toContainText('Reachable')

    await page.getByRole('button', { name: 'Add starter routes' }).click()
    await expect(page.getByLabel('Route 1 id')).toHaveValue('quick_question')

    // A route without a model must be rejected with a field error.
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByRole('alert').first()).toBeVisible()

    const firstModel = page.getByLabel('Route 1 primary model')
    const options = await firstModel.locator('option').allTextContents()
    expect(options.length).toBeGreaterThan(1)
    await firstModel.selectOption({ index: 1 })

    await page.getByLabel('Playground prompt').fill('what is a goroutine?')
    await page.getByRole('button', { name: /Route$/ }).click()
    await expect(page.getByTestId('ama-playground-result')).toBeVisible()

    // Remaining routes still lack models; fill them so Save succeeds.
    for (let i = 2; i <= 4; i++) {
      await page.getByLabel(`Route ${i} primary model`).selectOption({ index: 1 })
    }
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Auto mode settings saved')).toBeVisible()

    // Auto is the first entry of the model switcher.
    await page.goto('/')
    await page.keyboard.press('Control+o')
    await expect(page.locator('.ovl-model-name').first()).toContainText('Auto')
  })
})
