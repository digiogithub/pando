import { defineConfig } from '@playwright/test'

// E2E runs against an already started Pando (`pando app`, isolated HOME) given by
// PANDO_E2E_BASE_URL; specs skip themselves when it is not set.
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  use: {
    baseURL: process.env.PANDO_E2E_BASE_URL,
    channel: process.env.PANDO_E2E_CHANNEL || 'chrome',
    headless: true,
    // `pando app` serves a self-signed certificate.
    ignoreHTTPSErrors: true,
  },
})
