import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { registerSW } from 'virtual:pwa-register'
import '@/index.css'
import '@/i18n'
import App from './App'

// Register the PWA service worker with auto-update behaviour. `immediate: true`
// checks for a new service worker as soon as the page loads; combined with the
// `skipWaiting`/`clientsClaim` generated worker (registerType: 'autoUpdate'),
// the new worker takes control and `virtual:pwa-register` reloads the page on
// `controllerchange`. Without this explicit registration vite-plugin-pwa only
// injects a bare register script with no reload logic, so a freshly updated
// binary keeps serving the previous UI from the old worker until the desktop
// window is closed and reopened.
registerSW({ immediate: true })

const root = document.getElementById('root')!
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>
)
