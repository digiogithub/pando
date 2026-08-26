import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  base: process.env.VITE_BASE_URL || '/',
  define: {
    __APP_VERSION__: JSON.stringify(process.env.npm_package_version ?? '0.0.0'),
  },
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['pando-icon.svg', 'pando_mascot.svg', 'pwa-icon-192.png', 'pwa-icon-512.png'],
      manifest: {
        name: 'Pando AI Assistant',
        short_name: 'Pando',
        description: 'AI assistant designed to improve the workflow of software developers',
        theme_color: '#0a0a0f',
        background_color: '#0a0a0f',
        display: 'standalone',
        orientation: 'any',
        scope: '/',
        start_url: '/',
        icons: [
          {
            src: 'pando-icon.svg',
            sizes: 'any',
            type: 'image/svg+xml',
            purpose: 'any',
          },
          {
            src: 'pwa-icon-192.png',
            sizes: '192x192',
            type: 'image/png',
          },
          {
            src: 'pwa-icon-512.png',
            sizes: '512x512',
            type: 'image/png',
          },
          {
            src: 'pwa-icon-maskable.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff,woff2}'],
        runtimeCaching: [
          {
            urlPattern: /^\/api\/v1\/(sessions|config|models)/,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'pando-api-cache',
              expiration: { maxEntries: 50, maxAgeSeconds: 60 * 5 },
            },
          },
          {
            // SSE and streaming endpoints must never be cached
            urlPattern: /^\/api\/v1\/(chat|messages|stream)/,
            handler: 'NetworkOnly',
          },
        ],
      },
      devOptions: {
        enabled: true,
        type: 'module',
      },
    }),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
      // Resolved straight to source rather than through node_modules: the
      // package ships TypeScript, and aliasing keeps Vite's dep optimiser out
      // of a workspace package that changes as often as the app does.
      '@pando/client': resolve(__dirname, './packages/pando-client/src'),
    },
  },
  server: {
    host: '0.0.0.0',
    allowedHosts: ['lenovop3'],
    port: 5555,
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
