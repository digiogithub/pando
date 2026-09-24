import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { registerSW } from 'virtual:pwa-register'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import '@/index.css'
// Initialise the theme store early (attaches system-appearance listeners).
import '@/hooks/useTheme'
import '@/i18n'
import App from './App'
import { IconProvider, ICON_DEFAULTS } from '@/components/ui/icons'

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
    <IconProvider size={ICON_DEFAULTS.size} strokeWidth={ICON_DEFAULTS.strokeWidth}>
      <App />
    </IconProvider>
  </StrictMode>
)
