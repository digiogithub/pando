// Local dev helper: proxy to a `pando serve` backend running with its default self-signed TLS.
// Usage: bunx --bun vite --config vite.dev-https.config.ts --port <port>
import { mergeConfig } from 'vite'
import base from './vite.config'

const target = process.env.PANDO_API ?? 'https://localhost:8765'

export default mergeConfig(base, {
  server: {
    proxy: {
      '/api': { target, changeOrigin: true, secure: false, timeout: 0, proxyTimeout: 0 },
      '/health': { target, changeOrigin: true, secure: false },
    },
  },
})
