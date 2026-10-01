/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

// Allow CSS side-effect imports
declare module '*.css' {}

interface Window {
  __PANDO_API_BASE__?: string
  __PANDO_ROUTER_BASENAME__?: string
}
