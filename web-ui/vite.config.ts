import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8765',
        changeOrigin: true,
        // No timeout for SSE streams and long-running tool calls
        timeout: 0,
        proxyTimeout: 0,
        configure: (proxy) => {
          // Disable response buffering so SSE events are forwarded immediately
          proxy.on('proxyRes', (proxyRes) => {
            proxyRes.headers['x-accel-buffering'] = 'no'
          })
        },
      },
      '/health': {
        target: 'http://localhost:8765',
        changeOrigin: true,
      },
    },
  },
})
