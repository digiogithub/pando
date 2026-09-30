import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      { find: /^@pando\/client\/types$/, replacement: resolve(__dirname, 'packages/pando-client/src/types/index.ts') },
      { find: /^@pando\/client$/, replacement: resolve(__dirname, 'packages/pando-client/src/index.ts') },
      { find: /^@pando\/client\/(.*)$/, replacement: resolve(__dirname, 'packages/pando-client/src/$1') },
      { find: /^@\/(.*)$/, replacement: resolve(__dirname, 'src/$1') },
    ],
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    include: ['src/**/*.test.{ts,tsx}', 'packages/**/*.test.{ts,tsx}'],
  },
})
