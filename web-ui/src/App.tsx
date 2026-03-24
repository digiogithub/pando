import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Suspense } from 'react'
import MainLayout from '@/components/layout/MainLayout'
import LoadingSpinner from '@/components/shared/LoadingSpinner'
import ChatView from '@/components/chat/ChatView'
import SimpleChatView from '@/components/chat/SimpleChatView'
import SettingsView from '@/components/settings/SettingsView'
import LogsView from '@/components/logs/LogsView'
import OrchestratorView from '@/components/orchestrator/OrchestratorView'
import TerminalView from '@/components/terminal/TerminalView'
import SnapshotsView from '@/components/snapshots/SnapshotsView'
import EvaluatorView from '@/components/evaluator/EvaluatorView'
import CodeEditorView from '@/components/editor/CodeEditorView'

// Placeholder pages for routes (will be replaced in later phases)
function PlaceholderPage({ name }: { name: string }) {
  return (
    <div style={{ padding: '2rem', color: 'var(--fg)' }}>
      <h2 style={{ fontSize: 20, fontWeight: 600, marginBottom: '0.5rem' }}>{name}</h2>
      <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>This view will be implemented soon.</p>
    </div>
  )
}

function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={<div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}><LoadingSpinner size={32} /></div>}>
        <Routes>
          {/* Standalone — no layout */}
          <Route path="/chat/simple" element={<SimpleChatView />} />
          <Route path="/editor" element={<CodeEditorView />} />

          {/* Main layout */}
          <Route path="/" element={<MainLayout />}>
            <Route index element={<ChatView />} />
            <Route path="orchestrator" element={<OrchestratorView />} />
            <Route path="logs" element={<LogsView />} />
            <Route path="snapshots" element={<SnapshotsView />} />
            <Route path="evaluator" element={<EvaluatorView />} />
            <Route path="editor" element={<Navigate to="/editor" replace />} />
            <Route path="terminal" element={<TerminalView />} />
            <Route path="settings" element={<SettingsView />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}

export default App
