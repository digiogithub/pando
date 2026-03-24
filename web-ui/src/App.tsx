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
import EvaluatorView from '@/components/evaluator/EvaluatorView'
import CodeEditorView from '@/components/editor/CodeEditorView'
import { authenticate, checkHealth } from '@/services/auth'

function App() {
  const [splashStatus, setSplashStatus] = useState<SplashStatus>('connecting')
  const [showSplash, setShowSplash] = useState(true)

  const initApp = useCallback(async () => {
    setSplashStatus('connecting')
    try {
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
    initApp()
  }, [initApp])

  return (
    <>
      {showSplash && (
        <SplashScreen status={splashStatus} onDone={() => setShowSplash(false)} />
      )}

      <BrowserRouter>
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
                <Route path="evaluator" element={<EvaluatorView />} />
                <Route path="editor" element={<Navigate to="/editor" replace />} />
                <Route path="terminal" element={<TerminalView />} />
                <Route path="settings" element={<SettingsView />} />
                <Route path="*" element={<NotFound />} />
              </Route>

              {/* Top-level 404 */}
              <Route path="*" element={<NotFound />} />
            </Routes>
          </ErrorBoundary>
        </Suspense>
      </BrowserRouter>

      <ToastContainer />
    </>
  )
}

export default App
