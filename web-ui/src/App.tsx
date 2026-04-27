import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Suspense, useState, useEffect, useCallback } from 'react'
import MainLayout from '@/components/layout/MainLayout'
import LoadingSpinner from '@/components/shared/LoadingSpinner'
import ErrorBoundary from '@/components/shared/ErrorBoundary'
import NotFound from '@/components/shared/NotFound'
import { ToastContainer } from '@/components/shared/Toast'
import SplashScreen, { type SplashStatus } from '@/components/splash/SplashScreen'
import ChatView from '@/components/chat/ChatView'
import SimpleChatView from '@/components/chat/SimpleChatView'
import SettingsView from '@/components/settings/SettingsView'
import LogsView from '@/components/logs/LogsView'
import OrchestratorView from '@/components/orchestrator/OrchestratorView'
import TerminalView from '@/components/terminal/TerminalView'
import SnapshotsView from '@/components/snapshots/SnapshotsView'
import SelfImprovementView from '@/components/evaluator/SelfImprovementView'
import CodeEditorView from '@/components/editor/CodeEditorView'
import ProjectsView from '@/components/projects/ProjectsView'
import { authenticate, checkHealth } from '@/services/auth'
import { isDesktop, getDesktopConfig } from '@/services/desktop'
import { initDesktopMode } from '@/services/api'
import { useLanguageSync } from '@/hooks/useLanguageSync'
import PWAInstallPrompt from '@/components/shared/PWAInstallPrompt'
import { useNotificationsStore } from '@/stores/notificationsStore'

function App() {
  useLanguageSync()
  const [splashStatus, setSplashStatus] = useState<SplashStatus>('connecting')
  const [showSplash, setShowSplash] = useState(true)
  const connectNotifications = useNotificationsStore((s) => s.connect)
  const disconnectNotifications = useNotificationsStore((s) => s.disconnect)

  const initApp = useCallback(async () => {
    setSplashStatus('connecting')
    try {
      if (isDesktop) {
        const cfg = await getDesktopConfig()
        if (cfg) initDesktopMode(cfg)
      }
      const healthy = await checkHealth()
      if (!healthy) {
        setSplashStatus('error')
        return
      }
      setSplashStatus('authenticating')
      await authenticate()
      setSplashStatus('ready')
    } catch {
      setSplashStatus('error')
      // After 3 seconds on error, let the app through anyway (backend may be optional)
      setTimeout(() => setShowSplash(false), 3000)
    }
  }, [])

  useEffect(() => {
    void initApp()
  }, [initApp])

  // Connect to the notifications SSE stream once the app is ready.
  useEffect(() => {
    if (!showSplash && splashStatus === 'ready') {
      connectNotifications()
      return () => disconnectNotifications()
    }
  }, [showSplash, splashStatus, connectNotifications, disconnectNotifications])

  return (
    <>
      {showSplash && (
        <SplashScreen status={splashStatus} onDone={() => setShowSplash(false)} />
      )}

      {!showSplash && <BrowserRouter>
        <Suspense
          fallback={
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>
              <LoadingSpinner size={32} />
            </div>
          }
        >
          <ErrorBoundary>
            <Routes>
              {/* Standalone — no layout */}
              <Route path="/chat/simple" element={<SimpleChatView />} />
              <Route path="/editor" element={<CodeEditorView />} />

              {/* Main layout */}
              <Route path="/" element={<MainLayout />}>
                <Route index element={<ChatView />} />
                <Route path="chat" element={<ChatView />} />
                <Route path="orchestrator" element={<OrchestratorView />} />
                <Route path="logs" element={<LogsView />} />
                <Route path="snapshots" element={<SnapshotsView />} />
                <Route path="evaluator" element={<SelfImprovementView />} />
                <Route path="editor" element={<Navigate to="/editor" replace />} />
                <Route path="terminal" element={<TerminalView />} />
                <Route path="settings" element={<SettingsView />} />
                <Route path="projects" element={<ProjectsView />} />
                <Route path="*" element={<NotFound />} />
              </Route>

              {/* Top-level 404 */}
              <Route path="*" element={<NotFound />} />
            </Routes>
          </ErrorBoundary>
        </Suspense>
      </BrowserRouter>}

      <ToastContainer />
      <PWAInstallPrompt />
    </>
  )
}

export default App
